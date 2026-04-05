package search

// ReciprocalRankFusion merges BM25 and vector result lists using RRF.
// k is the rank constant (default 60 per the paper).
// Returns results sorted by descending RRF score.
func ReciprocalRankFusion(bm25 []Result, vec []Result, k int) []Result {
	if k <= 0 {
		k = 60
	}

	type score struct {
		result   Result
		rrfScore float64
		bm25Rank int
		vecRank  int
		bm25Sc   float64
		vecDist  float64
	}

	byChunk := map[int64]*score{}

	for i, r := range bm25 {
		rank := i + 1
		s, ok := byChunk[r.ChunkID]
		if !ok {
			byChunk[r.ChunkID] = &score{result: r, bm25Rank: rank, bm25Sc: r.Score}
			s = byChunk[r.ChunkID]
		}
		s.rrfScore += 1.0 / float64(k+rank)
		s.bm25Rank = rank
		s.bm25Sc = r.Score
	}

	for i, r := range vec {
		rank := i + 1
		s, ok := byChunk[r.ChunkID]
		if !ok {
			byChunk[r.ChunkID] = &score{result: r}
			s = byChunk[r.ChunkID]
		}
		s.rrfScore += 1.0 / float64(k+rank)
		s.vecRank = rank
		s.vecDist = 1.0/(r.Score+1e-9) - 1.0 // invert similarity back to distance
	}

	// Flatten and sort
	results := make([]Result, 0, len(byChunk))
	for _, s := range byChunk {
		r := s.result
		r.Score = s.rrfScore
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
