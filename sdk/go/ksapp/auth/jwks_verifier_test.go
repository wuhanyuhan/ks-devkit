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

// testJWKSServer 起一个 httptest server 模拟 keystone 的 /.well-known/jwks.json。
func testJWKSServer(t *testing.T, pub *rsa.PublicKey, kid string) *httptest.Server {
	t.Helper()
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	jwks := map[string]any{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"kid": kid,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
}

func signRS256(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestJWKSVerifier_VerifyValidToken(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := testJWKSServer(t, &priv.PublicKey, "key1")
	defer ts.Close()

	token := signRS256(t, priv, "key1", jwt.MapClaims{
		"sub": "user-42",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})

	v := NewJWKSVerifier(ts.URL)
	claims, err := v.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims["sub"] != "user-42" {
		t.Errorf("sub claim: got %v", claims["sub"])
	}
}

func TestJWKSVerifier_RejectExpired(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := testJWKSServer(t, &priv.PublicKey, "key1")
	defer ts.Close()

	token := signRS256(t, priv, "key1", jwt.MapClaims{
		"sub": "user-42",
		"exp": time.Now().Add(-5 * time.Minute).Unix(),
	})

	v := NewJWKSVerifier(ts.URL)
	if _, err := v.Verify(token); err == nil {
		t.Fatal("expired token 应被拒绝")
	}
}

func TestJWKSVerifier_RejectUnknownKid(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	ts := testJWKSServer(t, &priv.PublicKey, "key1")
	defer ts.Close()

	token := signRS256(t, priv, "unknown-kid", jwt.MapClaims{
		"sub": "user-42",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})

	v := NewJWKSVerifier(ts.URL)
	if _, err := v.Verify(token); err == nil {
		t.Fatal("未知 kid 应被拒绝")
	}
}

func TestJWKSVerifier_CacheHit(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	fetches := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		nBytes := priv.PublicKey.N.Bytes()
		eBytes := big.NewInt(int64(priv.PublicKey.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA", "kid": "key1", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(nBytes),
				"e": base64.RawURLEncoding.EncodeToString(eBytes),
			}},
		})
	}))
	defer ts.Close()

	v := NewJWKSVerifier(ts.URL)
	token := signRS256(t, priv, "key1", jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()})

	for i := 0; i < 3; i++ {
		if _, err := v.Verify(token); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
	if fetches != 1 {
		t.Errorf("JWKS 应只拉取 1 次（缓存命中），实际 %d 次", fetches)
	}
}

func TestJWKSVerifier_RejectNoneAlgorithm(t *testing.T) {
	// 防止 alg=none 攻击
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
	}))
	defer ts.Close()

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": "x"})
	s, _ := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)

	v := NewJWKSVerifier(ts.URL)
	if _, err := v.Verify(s); err == nil {
		t.Fatal("alg=none 的 token 必须被拒绝")
	}
}
