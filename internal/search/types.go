package search

import (
	"net/url"
	"strings"

	"github.com/itsmostafa/qi/internal/config"
)

// Passage is an additional matched chunk belonging to a search result.
type Passage struct {
	ChunkID     int64  `json:"chunk_id"`
	HeadingPath string `json:"heading_path"`
	Snippet     string `json:"snippet"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
}

// passageOf projects a result's own chunk into a supporting passage.
func passageOf(r Result) Passage {
	return Passage{
		ChunkID: r.ChunkID, HeadingPath: r.HeadingPath, Snippet: r.Snippet,
		StartLine: r.StartLine, EndLine: r.EndLine,
	}
}

// SourceURI returns a stable, URL-escaped source identity for a path in a
// collection. Slashes delimit path segments; characters within a segment are
// escaped using net/url semantics.
func SourceURI(collection, path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return "qi://" + collection + "/" + strings.Join(segments, "/")
}

// Result is a single search hit.
type Result struct {
	DocID       int64   `json:"doc_id"`
	ChunkID     int64   `json:"chunk_id"`
	Collection  string  `json:"collection"`
	Path        string  `json:"path"`
	SourceURI   string  `json:"source_uri"`
	Hash        string  `json:"hash"`
	Title       string  `json:"title"`
	HeadingPath string  `json:"heading_path"`
	Snippet     string  `json:"snippet"`
	StartLine   int     `json:"start_line"`
	EndLine     int     `json:"end_line"`
	Timestamp   string  `json:"timestamp"`
	Score       float64 `json:"score"`
	// Scale names the unit of Score. BM25 magnitudes and RRF scores differ by
	// two orders of magnitude and are not comparable across commands.
	Scale    string        `json:"scale"`
	Explain  *ScoreExplain `json:"explain,omitempty"`
	Passages []Passage     `json:"passages,omitempty"`
}

// ScoreExplain breaks down how a score was computed.
type ScoreExplain struct {
	BM25Score  float64 `json:"bm25_score,omitempty"`
	BM25Rank   int     `json:"bm25_rank,omitempty"`
	VectorDist float64 `json:"vector_distance,omitempty"`
	VectorRank int     `json:"vector_rank,omitempty"`
	RRFScore   float64 `json:"rrf_score,omitempty"`
}

// MaxPassages bounds the additional evidence returned per document.
const MaxPassages = 5

func passageLimit(opts SearchOpts) int {
	return min(max(opts.Passages, 0), MaxPassages)
}

// SearchOpts configures a search operation.
type SearchOpts struct {
	Query      string
	Collection string // empty = all collections
	TopK       int
	Pool       int    // candidates to retrieve before dedupe/cap; defaults to TopK
	Mode       string // lexical | hybrid | deep
	Explain    bool
	Since      string // YYYY-MM-DD, inclusive lower bound on document timestamp
	Until      string // YYYY-MM-DD, inclusive upper bound
	Sort       string // "" = relevance, "date" = newest first
	Path       string // glob over the collection-relative document path
	Passages   int    // additional matched chunks per result, capped at 5
}

// inScope reports whether a document path passes the caller's scope glob. It
// is applied while candidates are still being collected, so an in-scope
// document below a page of out-of-scope hits still reaches the result set.
func inScope(pattern, docPath string) bool {
	return pattern == "" || config.PathMatch(pattern, docPath)
}
