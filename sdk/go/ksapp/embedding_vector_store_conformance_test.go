package ksapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type relayFixture struct {
	Request struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	} `json:"request"`
	Response map[string]any `json:"response"`
}

func loadRelayFixture(t *testing.T, name string) relayFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "shared-fixtures", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f relayFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return f
}

func TestEmbeddingClientConformanceFixture(t *testing.T) {
	f := loadRelayFixture(t, "embeddings_v1.json")
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != f.Request.Method || r.URL.Path != f.Request.Path {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(f.Response)
	}))
	defer srv.Close()

	c := &EmbeddingClient{gatewayURL: srv.URL, relayToken: "tk", model: "bge-m3", httpClient: srv.Client()}
	got, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(captured, f.Request.Body) {
		t.Fatalf("body=%#v want %#v", captured, f.Request.Body)
	}
	if got.Tokens != 2 || !reflect.DeepEqual(got.Dense, []float32{0.1, 0.2}) {
		t.Fatalf("unexpected embedding result: %#v", got)
	}
	if !reflect.DeepEqual(got.Sparse, map[string]float32{"100": 0.5, "7": 0.25}) {
		t.Fatalf("unexpected sparse embedding: %#v", got.Sparse)
	}
}

func TestVectorStoreClientConformanceFixture(t *testing.T) {
	f := loadRelayFixture(t, "vector_store_search_text.json")
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != f.Request.Method || r.URL.Path != f.Request.Path {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(f.Response)
	}))
	defer srv.Close()

	emb := &EmbeddingClient{gatewayURL: srv.URL, relayToken: "tk", model: "bge-m3", httpClient: srv.Client()}
	got, err := newVectorStoreClient(emb, "documents").SearchText(context.Background(), "hello", SearchOptions{TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(captured, f.Request.Body) {
		t.Fatalf("body=%#v want %#v", captured, f.Request.Body)
	}
	if len(got) != 1 || got[0].ID != "doc1" || got[0].Payload["doc_id"] != "doc1" {
		t.Fatalf("unexpected search result: %#v", got)
	}
}

func TestLLMChatIntentConformanceFixture(t *testing.T) {
	f := loadRelayFixture(t, "relay_chat_intent.json")
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != f.Request.Method || r.URL.Path != f.Request.Path {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(f.Response)
	}))
	defer srv.Close()

	c := &LLMClient{gatewayURL: srv.URL, relayToken: "tk", httpClient: srv.Client()}
	if _, err := c.Chat(context.Background(), ChatRequest{
		Messages:            []Message{{Role: "user", Content: "hi"}},
		Tier:                "flagship",
		RequireCapabilities: []string{"vision"},
		Reasoning:           "on",
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(captured, f.Request.Body) {
		t.Fatalf("intent wire body 不一致:\n got=%#v\nwant=%#v", captured, f.Request.Body)
	}
}
