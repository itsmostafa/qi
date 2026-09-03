package search

// Result is a single search hit.
type Result struct {
	DocID       int64   `json:"doc_id"`
	ChunkID     int64   `json:"chunk_id"`
	Collection  string  `json:"collection"`
	Path        string  `json:"path"`
	Title       string  `json:"title"`
	HeadingPath string  `json:"heading_path"`
	Snippet     string  `json:"snippet"`
	Timestamp   string  `json:"timestamp"`
	Score       float64 `json:"score"`
	// Scale names the unit of Score. BM25 magnitudes and RRF scores differ by
	// two orders of magnitude and are not comparable across commands.
	Scale   string        `json:"scale"`
	Explain *ScoreExplain `json:"explain,omitempty"`
}

// ScoreExplain breaks down how a score was computed.
type ScoreExplain struct {
	BM25Score   float64 `json:"bm25_score,omitempty"`
	BM25Rank    int     `json:"bm25_rank,omitempty"`
	VectorDist  float64 `json:"vector_distance,omitempty"`
	VectorRank  int     `json:"vector_rank,omitempty"`
	RRFScore    float64 `json:"rrf_score,omitempty"`
	RerankScore float64 `json:"rerank_score,omitempty"`
}

// SearchOpts configures a search operation.
type SearchOpts struct {
	Query      string
	Collection string // empty = all collections
	TopK       int
	Pool       int // candidates to retrieve before dedupe/cap; defaults to TopK
	Mode       string // lexical | hybrid | deep
	Explain    bool
	Since      string // YYYY-MM-DD, inclusive lower bound on document timestamp
	Until      string // YYYY-MM-DD, inclusive upper bound
	Sort       string // "" = relevance, "date" = newest first
}
