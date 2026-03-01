package shardnode

type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type SearchHit struct {
	DocID   string  `json:"doc_id"`
	Score   float64 `json:"score"`
	ShardID string  `json:"shard_id"`
	Title   string  `json:"title"`
	Text    string  `json:"text"`
}
type SearchResponse struct {
	Hits []SearchHit `json:"hits"`
}
