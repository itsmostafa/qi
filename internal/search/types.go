package search

// Result is a single search hit.
type Result struct {
	DocID       int64   `json:"doc_id"`
	ChunkID     int64   `json:"chunk_id"`
	Collection  string  `json:"collection"`
	Path        string  `json:"path"`
	Title       string  `json:"title,omitempty"`
	HeadingPath string  `json:"heading_path,omitempty"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	Explain     *ScoreExplain `json:"explain,omitempty"`
}

// ScoreExplain breaks down how a score was computed.
type ScoreExplain struct {
	BM25Score    float64 `json:"bm25_score,omitempty"`
	BM25Rank     int     `json:"bm25_rank,omitempty"`
	VectorDist   float64 `json:"vector_distance,omitempty"`
	VectorRank   int     `json:"vector_rank,omitempty"`
	RRFScore     float64 `json:"rrf_score,omitempty"`
	RerankScore  float64 `json:"rerank_score,omitempty"`
}

// SearchOpts configures a search operation.
type SearchOpts struct {
	Query      string
	Collection string // empty = all collections
	TopK       int
	Mode       string // lexical | hybrid | deep
	Explain    bool
}
