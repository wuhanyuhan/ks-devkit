package ksapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestCapabilityHTTPEndpointValidTokenRoutesToHandler(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: sync
      timeout_ms: 30000
      backend:
        kind: http_endpoint
        path: /capabilities/foo
        method: POST
`)
	priv, pubPEM := makeRSAKeypair(t)
	app := New("ks-mcp-x", WithManifest(manifest))
	if err := app.SetScopedJWTTestKey("test-kid", pubPEM); err != nil {
		t.Fatal(err)
	}
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return map[string]any{
			"echo":         args["input"],
			"user_id":      ctx.UserID(),
			"app_id":       ctx.CallerID(),
			"chain":        ctx.ChainID(),
			"chain_header": ctx.ChainHeader(),
			"request":      ctx.RequestID(),
		}, nil
	})

	mux := app.Mux()
	now := time.Now().Unix()
	tokenStr := signRS256(t, priv, jwt.MapClaims{
		"iss": "keystone", "aud": "ks-mcp-x.foo",
		"sub": "u-100", "iat": now, "exp": now + 60,
		"kx_caller_id":   "ks-mcp-writer",
		"kx_caller_kind": "app",
		"kx_chain_id":    "chain-1",
		"kx_request_id":  "req-1",
	})

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"input": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/capabilities/foo", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set(headerCallChain, "encoded-chain")
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["echo"] != "hi" {
		t.Fatalf("echo=%v", got["echo"])
	}
	if got["user_id"] != "u-100" {
		t.Fatalf("user_id=%v", got["user_id"])
	}
	if got["app_id"] != "ks-mcp-writer" {
		t.Fatalf("app_id=%v", got["app_id"])
	}
	if got["chain"] != "chain-1" {
		t.Fatalf("chain=%v", got["chain"])
	}
	if got["chain_header"] != "encoded-chain" {
		t.Fatalf("chain_header=%v", got["chain_header"])
	}
	if got["request"] != "req-1" {
		t.Fatalf("request=%v", got["request"])
	}
}

func TestCapabilityHTTPEndpointWrongAudRejected(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: sync
      backend:
        kind: http_endpoint
        path: /capabilities/foo
        method: POST
`)
	priv, pubPEM := makeRSAKeypair(t)
	app := New("ks-mcp-x", WithManifest(manifest))
	if err := app.SetScopedJWTTestKey("test-kid", pubPEM); err != nil {
		t.Fatal(err)
	}
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return map[string]any{"x": 1}, nil
	})
	mux := app.Mux()
	now := time.Now().Unix()
	tokenStr := signRS256(t, priv, jwt.MapClaims{
		"iss": "keystone", "aud": "ks-mcp-x.OTHER",
		"sub": "u", "iat": now, "exp": now + 60,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/capabilities/foo", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	mux.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["error"] != "aud_mismatch" {
		t.Fatalf("error=%v", got["error"])
	}
}

func TestCapabilityHTTPEndpointMissingBearerRejected(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: sync
      backend:
        kind: http_endpoint
        path: /capabilities/foo
        method: POST
`)
	_, pubPEM := makeRSAKeypair(t)
	app := New("ks-mcp-x", WithManifest(manifest))
	if err := app.SetScopedJWTTestKey("test-kid", pubPEM); err != nil {
		t.Fatal(err)
	}
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return map[string]any{}, nil
	})
	mux := app.Mux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/capabilities/foo", bytes.NewReader([]byte(`{}`)))
	mux.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status=%d", rec.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["error"] != "missing_bearer" {
		t.Fatalf("error=%v", got["error"])
	}
}

func TestCapabilityHTTPEndpointExpiredTokenRejected(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: sync
      backend:
        kind: http_endpoint
        path: /capabilities/foo
        method: POST
`)
	priv, pubPEM := makeRSAKeypair(t)
	app := New("ks-mcp-x", WithManifest(manifest))
	if err := app.SetScopedJWTTestKey("test-kid", pubPEM); err != nil {
		t.Fatal(err)
	}
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return map[string]any{}, nil
	})
	mux := app.Mux()
	now := time.Now().Unix()
	tokenStr := signRS256(t, priv, jwt.MapClaims{
		"iss": "keystone", "aud": "ks-mcp-x.foo",
		"sub": "u", "iat": now - 120, "exp": now - 60,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/capabilities/foo", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	mux.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status=%d", rec.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["error"] != "token_expired" {
		t.Fatalf("error=%v", got["error"])
	}
}
