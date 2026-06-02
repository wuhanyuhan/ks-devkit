package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

func TestParseSlugAndVersion(t *testing.T) {
	cases := []struct {
		in       string
		wantSlug string
		wantVer  string
	}{
		{"ks-mcp-email", "ks-mcp-email", ""},
		{"ks-mcp-email@v0.5.2", "ks-mcp-email", "v0.5.2"},
		{"foo@1.2", "foo", "1.2"},
	}
	for _, tc := range cases {
		s, v := parseSlugAndVersion(tc.in)
		if s != tc.wantSlug || v != tc.wantVer {
			t.Errorf("%q → (%q, %q), want (%q, %q)", tc.in, s, v, tc.wantSlug, tc.wantVer)
		}
	}
}

func TestRunStatusListMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 期望 ListVersionsPaged：/v1/developer/apps/ks-mcp-email/versions?limit=10&offset=0
		if !strings.HasPrefix(r.URL.Path, "/v1/developer/apps/ks-mcp-email/versions") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"version":"v0.5.2","status":"approved","review_path":"fast-track","submitted_at":"2026-05-03T10:00:00Z"}],"total":1}}`))
	}))
	defer srv.Close()

	c := hub.NewClient(srv.URL, "tok")
	cred := &auth.Credentials{AuthType: auth.AuthTypePAT, AccessToken: "tok"}
	var buf bytes.Buffer
	err := runStatus(c, cred, "ks-mcp-email", "", false, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "v0.5.2") || !strings.Contains(out, "approved") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestRunStatusJSONMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"version":"v0.5.2","status":"approved","review_path":"fast-track"}}`))
	}))
	defer srv.Close()

	c := hub.NewClient(srv.URL, "tok")
	cred := &auth.Credentials{AuthType: auth.AuthTypePAT, AccessToken: "tok"}
	var buf bytes.Buffer
	err := runStatus(c, cred, "ks-mcp-email", "v0.5.2", true, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, buf.String())
	}
	if got["status"] != "approved" {
		t.Fatalf("got %+v", got)
	}
}
