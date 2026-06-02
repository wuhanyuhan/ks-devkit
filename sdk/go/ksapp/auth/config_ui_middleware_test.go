package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signConfigUIToken 签发一个带自定义 claims 的 RS256 JWT，供 RequireConfigUIJWT 测试用。
func signConfigUIToken(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(5 * time.Minute).Unix()
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return s
}

func TestRequireConfigUIJWT_MissingAuthorization(t *testing.T) {
	t.Parallel()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("缺 Authorization 应 401, got %d", rec.Code)
	}
}

func TestRequireConfigUIJWT_WrongScheme(t *testing.T) {
	t.Parallel()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("非 Bearer 应 401, got %d", rec.Code)
	}
}

func TestRequireConfigUIJWT_InvalidToken(t *testing.T) {
	t.Parallel()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	// 签发 token 用的 kid 不在 JWKS 里
	bad := signConfigUIToken(t, priv, "unknown-kid", jwt.MapClaims{
		"type":          "mcp_config_ui",
		"mcp_server_id": "42",
	})

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	req.Header.Set("Authorization", "Bearer "+bad)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未知 kid 应 401, got %d", rec.Code)
	}
}

func TestRequireConfigUIJWT_WrongType(t *testing.T) {
	t.Parallel()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	tok := signConfigUIToken(t, priv, "key1", jwt.MapClaims{
		"type":          "developer",
		"mcp_server_id": "42",
	})

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("type=developer 应 401, got %d", rec.Code)
	}
}

// TestRequireConfigUIJWT_EnvNotSet 使用 t.Setenv 清空环境变量，
// 不能加 t.Parallel()（与其他并行测试共享 env 会出状况）。
func TestRequireConfigUIJWT_EnvNotSet(t *testing.T) {
	t.Setenv("KSAPP_SERVER_ID", "")

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	tok := signConfigUIToken(t, priv, "key1", jwt.MapClaims{
		"type":          "mcp_config_ui",
		"mcp_server_id": "42",
	})

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("env 未配置应 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequireConfigUIJWT_ServerIDMismatch(t *testing.T) {
	t.Setenv("KSAPP_SERVER_ID", "100")

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	tok := signConfigUIToken(t, priv, "key1", jwt.MapClaims{
		"type":          "mcp_config_ui",
		"mcp_server_id": float64(42),
	})

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("server_id 不匹配应 403, got %d", rec.Code)
	}
}

func TestRequireConfigUIJWT_Success_FloatClaim(t *testing.T) {
	t.Setenv("KSAPP_SERVER_ID", "42")

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	tok := signConfigUIToken(t, priv, "key1", jwt.MapClaims{
		"type":          "mcp_config_ui",
		"mcp_server_id": float64(42),
		"sub":           "user-7",
	})

	var gotType, gotSub string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := ClaimsFromContext(r.Context()); ok {
			gotType, _ = c["type"].(string)
			gotSub, _ = c["sub"].(string)
		}
		w.WriteHeader(http.StatusOK)
	})

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(inner)
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("合法 float claim 应 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotType != "mcp_config_ui" {
		t.Errorf("claims.type 未进 ctx, got %q", gotType)
	}
	if gotSub != "user-7" {
		t.Errorf("claims.sub 未进 ctx, got %q", gotSub)
	}
}

func TestRequireConfigUIJWT_Success_StringClaim(t *testing.T) {
	t.Setenv("KSAPP_SERVER_ID", "42")

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := newTestJWKSServer(t, priv, "key1")
	defer ts.Close()

	tok := signConfigUIToken(t, priv, "key1", jwt.MapClaims{
		"type":          "mcp_config_ui",
		"mcp_server_id": "42",
	})

	h := RequireConfigUIJWT(NewJWKSVerifier(ts.URL))(okHandler())
	req := httptest.NewRequest("GET", "/config-ui/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("合法 string claim 应 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}
