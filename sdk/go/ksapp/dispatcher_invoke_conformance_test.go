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

func TestDispatcherInvokeOnBehalfOfConformanceFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "shared-fixtures", "dispatcher_invoke_on_behalf_of.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f struct {
		Request struct {
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"request"`
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != f.Request.Method || r.URL.Path != f.Request.Path {
			t.Fatalf("request %s %s, want %s %s", r.Method, r.URL.Path, f.Request.Method, f.Request.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
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
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	// 两侧都经 json.Unmarshal 到 map[string]any（数字均为 float64），可直接 DeepEqual
	if !reflect.DeepEqual(captured, f.Request.Body) {
		t.Fatalf("payload=%#v want %#v", captured, f.Request.Body)
	}
}
