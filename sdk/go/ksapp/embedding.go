package ksapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultEmbeddingTimeout = 60 * time.Second

// embeddingEncodingDenseSparse 让 bge-m3 同时返回 dense embedding 与 sparse_embedding。
const embeddingEncodingDenseSparse = "dense+sparse"

type EmbeddingClient struct {
	gatewayURL string
	relayToken string
	model      string
	dim        int
	httpClient *http.Client
}

type EmbeddingResult struct {
	Dense  []float32
	Sparse map[string]float32 // bge-m3 sparse 向量：token-id 串 → 权重；upsert 时可直接塞进 Point.Sparse
	Tokens int
}

func newEmbeddingClient() *EmbeddingClient {
	gatewayURL := os.Getenv("KS_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:9988"
	}
	dim, _ := strconv.Atoi(os.Getenv("KS_EMBEDDING_DIM"))
	return &EmbeddingClient{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		relayToken: firstEnv("KS_RELAY_TOKEN", "KEYSTONE_RELAY_TOKEN"),
		model:      os.Getenv("KS_EMBEDDING_MODEL"),
		dim:        dim,
		httpClient: &http.Client{Timeout: defaultEmbeddingTimeout},
	}
}

func (c *EmbeddingClient) Model() string { return c.model }

func (c *EmbeddingClient) Dim() int { return c.dim }

func (c *EmbeddingClient) Embed(ctx context.Context, text string) (*EmbeddingResult, error) {
	out, err := c.EmbedMany(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("embedding response is empty")
	}
	return out[0], nil
}

func (c *EmbeddingClient) EmbedMany(ctx context.Context, texts []string) ([]*EmbeddingResult, error) {
	if c.relayToken == "" {
		return nil, NewErrNotConfigured("embedding-relay", "KS_RELAY_TOKEN 未设置")
	}
	if c.model == "" {
		return nil, NewErrNotConfigured("embedding-relay", "KS_EMBEDDING_MODEL 未设置")
	}
	body := map[string]any{"model": c.model, "input": texts, "encoding_format": embeddingEncodingDenseSparse}
	respBody, err := c.postJSON(ctx, "/v1/mcp/relay/embeddings", body)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Data []struct {
			Index           int                `json:"index"`
			Embedding       []float32          `json:"embedding"`
			SparseEmbedding map[string]float32 `json:"sparse_embedding"`
		} `json:"data"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("解析 embedding 响应: %w", err)
	}
	results := make([]*EmbeddingResult, len(texts))
	tokens := 0
	if len(texts) > 0 {
		tokens = raw.Usage.TotalTokens / len(texts)
	}
	for _, item := range raw.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("embedding 响应 index 越界: %d", item.Index)
		}
		results[item.Index] = &EmbeddingResult{Dense: item.Embedding, Sparse: item.SparseEmbedding, Tokens: tokens}
	}
	return results, nil
}

func (c *EmbeddingClient) postJSON(ctx context.Context, path string, body any) ([]byte, error) {
	if c.relayToken == "" {
		return nil, NewErrNotConfigured("keystone-relay", "KS_RELAY_TOKEN 未设置")
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gatewayURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("创建请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.relayToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 Keystone relay: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyHTTPError(resp.StatusCode, respBody)
	}
	return respBody, nil
}
