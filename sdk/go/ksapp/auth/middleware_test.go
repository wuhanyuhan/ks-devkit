package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestJWKSServer(t *testing.T, priv *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nBytes := priv.PublicKey.N.Bytes()
		eBytes := big.NewInt(int64(priv.PublicKey.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(nBytes),
				"e": base64.RawURLEncoding.EncodeToString(eBytes),
			}},
		})
	}))
}

func signToken(t *testing.T, priv *rsa.PrivateKey, kid string) string {
	t.Helper()
	return signTokenWithClaims(t, priv, kid, jwt.MapClaims{
		"sub": "user-42",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
}

func signTokenWithClaims(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(5 * time.Minute).Unix()
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, _ := tok.SignedString(priv)
	return s
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestRequireJWT_ValidToken(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	mw := RequireJWT(NewJWKSVerifier(ts.URL))
	h := mw(okHandler())

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+signToken(t, priv, "key1"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("应放行：code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequireJWT_MissingHeader(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	h := RequireJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("POST", "/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("缺 Authorization 头应 401, got %d", rec.Code)
	}
}

func TestRequireJWT_WrongScheme(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	h := RequireJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("非 Bearer 应 401, got %d", rec.Code)
	}
}

func TestRequireJWT_InvalidToken(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	h := RequireJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("无效 token 应 401, got %d", rec.Code)
	}
}

func TestRequireJWT_ClaimsInContext(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	var gotSub string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := ClaimsFromContext(r.Context()); ok {
			gotSub, _ = c["sub"].(string)
		}
		w.WriteHeader(200)
	})

	h := RequireJWT(NewJWKSVerifier(ts.URL))(inner)
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+signToken(t, priv, "key1"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotSub != "user-42" {
		t.Errorf("claims 未进 context，got sub=%q", gotSub)
	}
}

func TestRequireJWT_ConfigUITokenServerIDMatch(t *testing.T) {
	t.Setenv("KSAPP_SERVER_ID", "42")
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	h := RequireJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+signTokenWithClaims(t, priv, "key1", jwt.MapClaims{
		"sub":           "user-42",
		"type":          "mcp_config_ui",
		"mcp_server_id": float64(42),
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("server_id 匹配应放行：code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequireJWT_ConfigUITokenServerIDMismatch(t *testing.T) {
	t.Setenv("KSAPP_SERVER_ID", "42")
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	h := RequireJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+signTokenWithClaims(t, priv, "key1", jwt.MapClaims{
		"sub":           "user-42",
		"type":          "mcp_config_ui",
		"mcp_server_id": float64(999),
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("server_id 不匹配应 403：code=%d body=%s", rec.Code, rec.Body.String())
	}
}
