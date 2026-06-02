package ksapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVectorStoreClientSearchText(t *testing.T) {
	var capturedPath string
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"results":[{"id":"doc1","score":0.92,"payload":{"doc_id":"doc1"}}]}`))
	}))
	defer srv.Close()

	emb := &EmbeddingClient{gatewayURL: srv.URL, relayToken: "tk", model: "bge-m3", httpClient: srv.Client()}
	store := newVectorStoreClient(emb, "documents")
	got, err := store.SearchText(context.Background(), "hello", SearchOptions{TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	if capturedPath != "/v1/mcp/relay/vector_store/search_text" {
		t.Fatalf("path=%s", capturedPath)
	}
	if captured["collection"] != "documents" || captured["text"] != "hello" {
		t.Fatalf("body=%#v", captured)
	}
	if len(got) != 1 || got[0].ID != "doc1" {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestVectorStoreClientCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"count":7}`))
	}))
	defer srv.Close()
	emb := &EmbeddingClient{gatewayURL: srv.URL, relayToken: "tk", model: "bge-m3", httpClient: srv.Client()}
	store := newVectorStoreClient(emb, "documents")
	n, err := store.Count(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("count=%d", n)
	}
}

func TestVectorStoreClientUpsertSerializesDenseSparse(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"status":"ok","upserted":1}`))
	}))
	defer srv.Close()

	emb := &EmbeddingClient{gatewayURL: srv.URL, relayToken: "tk", model: "bge-m3", httpClient: srv.Client()}
	store := newVectorStoreClient(emb, "documents")
	if err := store.Upsert(context.Background(), []Point{
		{ID: "p1", Dense: []float32{0.1, 0.2}, Sparse: map[string]float32{"3": 0.9}},
	}); err != nil {
		t.Fatal(err)
	}
	pt := captured["points"].([]any)[0].(map[string]any)
	if pt["id"] != "p1" {
		t.Fatalf("id=%v", pt["id"])
	}
	if _, ok := pt["dense"]; !ok {
		t.Fatalf("missing dense: %#v", pt)
	}
	if _, ok := pt["sparse"]; !ok {
		t.Fatalf("missing sparse: %#v", pt)
	}
	if _, ok := pt["vector"]; ok {
		t.Fatalf("legacy vector field must be gone: %#v", pt)
	}
}
