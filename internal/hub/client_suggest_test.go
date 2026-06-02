package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_SuggestManifest_OK(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/devkit/manifest/suggest" {
			t.Errorf("path = %q, want /v1/developer/devkit/manifest/suggest", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req SuggestManifestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.AppID != "skill-tdd" || len(req.MissingFields) != 2 {
			t.Errorf("req = %+v", req)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{
			"suggestions":{"summary":"摘要","tags":["a","b"]},
			"rationale":"...","confidence":0.85,
			"llm_model":"claude-haiku-4-5","prompt_version":"v1"
		},"message":""}`))
	}))
	defer mock.Close()

	c := NewClient(mock.URL, "test-token")
	out, err := c.SuggestManifest(SuggestManifestRequest{
		AppID:         "skill-tdd",
		SkillMdText:   "...",
		MissingFields: []string{"summary", "tags"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Suggestions.Summary != "摘要" {
		t.Errorf("summary = %q", out.Suggestions.Summary)
	}
	if len(out.Suggestions.Tags) != 2 {
		t.Errorf("tags = %v", out.Suggestions.Tags)
	}
	if out.Confidence != 0.85 || out.LLMModel != "claude-haiku-4-5" {
		t.Errorf("meta = %+v", out)
	}
}

func TestClient_SuggestManifest_503Degraded(t *testing.T) {
	// hub 503 → 客户端报错；调用方据此走 inline editor，不重试
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer mock.Close()
	c := NewClient(mock.URL, "x")
	_, err := c.SuggestManifest(SuggestManifestRequest{})
	if err == nil {
		t.Error("expected error on 503")
	}
}

func TestClient_SuggestManifest_BusinessError(t *testing.T) {
	// hub 返业务码 != 0：APIError
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":50301,"message":"LLM 服务降级"}`))
	}))
	defer mock.Close()
	c := NewClient(mock.URL, "x")
	_, err := c.SuggestManifest(SuggestManifestRequest{})
	if err == nil {
		t.Fatal("expected APIError")
	}
}

func TestClient_ParseChangelog_Found(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/developer/devkit/changelog/parse" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"parsed":{"found":true,"version_section":"### Added\n- x"}},"message":""}`))
	}))
	defer mock.Close()
	c := NewClient(mock.URL, "x")
	out, err := c.ParseChangelog(ParseChangelogRequest{
		AppID:           "x",
		Version:         "0.3.0",
		ChangelogMDText: "## [0.3.0]\n### Added\n- x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Parsed.Found || out.Parsed.VersionSection == "" {
		t.Errorf("got %+v", out)
	}
}

func TestClient_ParseChangelog_NotFound(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"parsed":{"found":false}},"message":""}`))
	}))
	defer mock.Close()
	c := NewClient(mock.URL, "x")
	out, err := c.ParseChangelog(ParseChangelogRequest{Version: "9.9.9", ChangelogMDText: "## [0.1.0]"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Parsed.Found {
		t.Error("expected found=false")
	}
}
