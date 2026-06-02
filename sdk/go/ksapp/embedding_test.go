package ksapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddingClientEmbedMany(t *testing.T) {
	var capturedPath, capturedAuth string
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"object":"list","model":"bge-m3","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2],"sparse_embedding":{"5":0.7}}],"usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer srv.Close()

	c := &EmbeddingClient{gatewayURL: srv.URL, relayToken: "tk", model: "bge-m3", dim: 1024, httpClient: srv.Client()}
	got, err := c.EmbedMany(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if capturedPath != "/v1/mcp/relay/embeddings" {
		t.Fatalf("path=%s", capturedPath)
	}
	if capturedAuth != "Bearer tk" {
		t.Fatalf("auth=%s", capturedAuth)
	}
	if captured["model"] != "bge-m3" || captured["encoding_format"] != "dense+sparse" {
		t.Fatalf("body=%#v", captured)
	}
	if len(got) != 1 || len(got[0].Dense) != 2 || got[0].Tokens != 2 {
		t.Fatalf("unexpected result: %#v", got)
	}
	if got[0].Sparse["5"] != 0.7 {
		t.Fatalf("sparse not parsed: %#v", got[0].Sparse)
	}
}

func TestEmbeddingClientNoToken(t *testing.T) {
	c := &EmbeddingClient{gatewayURL: "http://x", model: "bge-m3"}
	if _, err := c.Embed(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}
