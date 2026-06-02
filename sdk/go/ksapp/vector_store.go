package ksapp

import (
	"context"
	"encoding/json"
)

type Point struct {
	ID      string             `json:"id"`
	Dense   []float32          `json:"dense"`
	Sparse  map[string]float32 `json:"sparse,omitempty"` // bge-m3 sparse：token-id 串 → 权重；可直接取自 EmbeddingResult.Sparse
	Payload map[string]any     `json:"payload,omitempty"`
}

type Filter map[string]any

// SearchOptions 控制检索。
type SearchOptions struct {
	TopK   int
	Filter Filter
}

type SearchResult struct {
	ID      string         `json:"id"`
	Score   float32        `json:"score"`
	Payload map[string]any `json:"payload,omitempty"`
}

type VectorStoreClient struct {
	embedding  *EmbeddingClient
	collection string
}

func newVectorStoreClient(embedding *EmbeddingClient, collection string) *VectorStoreClient {
	return &VectorStoreClient{embedding: embedding, collection: collection}
}

func (s *VectorStoreClient) Upsert(ctx context.Context, points []Point) error {
	_, err := s.embedding.postJSON(ctx, "/v1/mcp/relay/vector_store/upsert", map[string]any{
		"collection": s.collection,
		"points":     points,
	})
	return err
}

// SearchText 由服务端 embed dense+sparse 后做 RRF hybrid 检索（托管向量链唯一检索路径）。
func (s *VectorStoreClient) SearchText(ctx context.Context, text string, opts SearchOptions) ([]SearchResult, error) {
	body := map[string]any{"collection": s.collection, "text": text}
	if opts.TopK > 0 {
		body["top_k"] = opts.TopK
	}
	if len(opts.Filter) > 0 {
		body["filter"] = opts.Filter
	}
	return s.search(ctx, "/v1/mcp/relay/vector_store/search_text", body)
}

func (s *VectorStoreClient) Delete(ctx context.Context, ids []string) error {
	_, err := s.embedding.postJSON(ctx, "/v1/mcp/relay/vector_store/delete", map[string]any{
		"collection": s.collection,
		"ids":        ids,
	})
	return err
}

func (s *VectorStoreClient) DeleteByFilter(ctx context.Context, filter Filter) error {
	_, err := s.embedding.postJSON(ctx, "/v1/mcp/relay/vector_store/delete", map[string]any{
		"collection": s.collection,
		"filter":     filter,
	})
	return err
}

func (s *VectorStoreClient) Count(ctx context.Context, filter Filter) (int, error) {
	body := map[string]any{"collection": s.collection}
	if len(filter) > 0 {
		body["filter"] = filter
	}
	resp, err := s.embedding.postJSON(ctx, "/v1/mcp/relay/vector_store/count", body)
	if err != nil {
		return 0, err
	}
	var out struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

func (s *VectorStoreClient) search(ctx context.Context, path string, body map[string]any) ([]SearchResult, error) {
	resp, err := s.embedding.postJSON(ctx, path, body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}
