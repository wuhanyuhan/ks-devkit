package ksapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDispatcherInvokeWithChainConformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "shared-fixtures", "dispatcher_invoke_with_chain.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f struct {
		Request struct {
			Method  string            `json:"method"`
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		} `json:"request"`
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	var capturedBody map[string]any
	capturedHeaders := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		for k := range f.Request.Headers {
			capturedHeaders[k] = r.Header.Get(k)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.Response)
	}))
	defer srv.Close()

	client := NewDispatcherClient(srv.URL, "tk")
	_, err = client.Invoke(context.Background(), InvokeOptions{
		Capability:       f.Request.Body["capability"].(string),
		Args:             f.Request.Body["args"].(map[string]any),
		Mode:             f.Request.Body["mode"].(string),
		OnBehalfOfUserID: int64(f.Request.Body["on_behalf_of_user_id"].(float64)),
		ChainID:          f.Request.Headers["X-Keystone-Chain-Id"],
		ChainHeader:      f.Request.Headers["X-Keystone-Call-Chain"],
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !reflect.DeepEqual(capturedBody, f.Request.Body) {
		t.Fatalf("body=%#v want %#v", capturedBody, f.Request.Body)
	}
	if !reflect.DeepEqual(capturedHeaders, f.Request.Headers) {
		t.Fatalf("headers=%#v want %#v", capturedHeaders, f.Request.Headers)
	}
}
