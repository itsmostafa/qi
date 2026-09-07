package search

import (
	"path/filepath"
	"strings"
)

// applyExtensionBoost multiplies scores for results whose file extension is in
// the preferred set, then re-sorts. boost==0 uses a default of 2.0.
func applyExtensionBoost(results []Result, exts []string, boost float64) []Result {
	if len(exts) == 0 {
		return results
	}
	if boost <= 0 {
		boost = 2.0
	}
	set := make(map[string]bool, len(exts))
	for _, e := range exts {
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		set[strings.ToLower(e)] = true
	}
	for i := range results {
		ext := strings.ToLower(filepath.Ext(results[i].Path))
		if set[ext] {
			results[i].Score *= boost
		}
	}
	sortByScore(results)
	return results
}

// ReciprocalRankFusion merges BM25 and vector result lists using RRF, fusing by
// document: each list contributes a document once, at its best rank, so a
// verbose file cannot buy rank with extra matching chunks or take extra slots.
// k is the rank constant (default 60 per the paper).
// Returns results sorted by descending RRF score. Both route primaries and
// supporting passages become evidence candidates, but only one document
// position per route contributes to the RRF score.
func ReciprocalRankFusion(bm25 []Result, vec []Result, k int, maxPassages int) []Result {
	passageCap := max(0, min(maxPassages, MaxPassages))
	if k <= 0 {
		k = 60
	}

	type score struct {
		result      Result
		rrfScore    float64
		bm25Rank    int
		vecRank     int
		bm25Sc      float64
		vecDist     float64
		evidence    []Passage
		evidenceIDs map[int64]bool
	}

	primaryPassage := func(r Result) Passage {
		return Passage{
			ChunkID: r.ChunkID, HeadingPath: r.HeadingPath, Snippet: r.Snippet,
			StartLine: r.StartLine, EndLine: r.EndLine,
		}
	}
	addEvidence := func(s *score, passages ...Passage) {
		if passageCap == 0 {
			return
		}
		for _, p := range passages {
			if s.evidenceIDs[p.ChunkID] {
				continue
			}
			if s.evidenceIDs == nil {
				s.evidenceIDs = map[int64]bool{}
			}
			s.evidenceIDs[p.ChunkID] = true
			s.evidence = append(s.evidence, p)
		}
	}

	byDoc := map[int64]*score{}
	for i, r := range bm25 {
		s, ok := byDoc[r.DocID]
		if ok {
			addEvidence(s, r.Passages...)
			continue // already counted at a better rank
		}
		rank := i + 1
		s = &score{
			result:   r,
			rrfScore: 1.0 / float64(k+rank),
			bm25Rank: rank,
			bm25Sc:   r.Score,
		}
		byDoc[r.DocID] = s
		addEvidence(s, primaryPassage(r))
		addEvidence(s, r.Passages...)
	}

	for i, r := range vec {
		rank := i + 1
		s, ok := byDoc[r.DocID]
		if !ok {
			s = &score{result: r}
			byDoc[r.DocID] = s
		} else if s.vecRank > 0 {
			addEvidence(s, r.Passages...)
			continue // already counted at a better rank
		} else if s.bm25Rank == 0 || rank < s.bm25Rank {
			// Show the chunk the better-ranked list picked, or the snippet
			// contradicts the ranks reported beside it.
			s.result = r
		}
		addEvidence(s, primaryPassage(r))
		addEvidence(s, r.Passages...)
		s.rrfScore += 1.0 / float64(k+rank)
		s.vecRank = rank
		s.vecDist = 1.0/(r.Score+1e-9) - 1.0 // invert similarity back to distance
	}

	// Flatten and sort. Remove the winning primary before applying the cap so
	// the other route's primary can still fill the final evidence slots.
	results := make([]Result, 0, len(byDoc))
	for _, s := range byDoc {
		r := s.result
		r.Score = s.rrfScore
		r.Scale = ScaleRRF
		r.Passages = nil
		for _, p := range s.evidence {
			if p.ChunkID == r.ChunkID {
				continue
			}
			r.Passages = append(r.Passages, p)
			if len(r.Passages) >= passageCap {
				break
			}
		}
		if r.Explain != nil || s.bm25Rank > 0 || s.vecRank > 0 {
			r.Explain = &ScoreExplain{
				BM25Score:  s.bm25Sc,
				BM25Rank:   s.bm25Rank,
				VectorDist: s.vecDist,
				VectorRank: s.vecRank,
				RRFScore:   s.rrfScore,
			}
		}
		results = append(results, r)
	}

	sortByScore(results)
	return results
}

func sortByScore(results []Result) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
