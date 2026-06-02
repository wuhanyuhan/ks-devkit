package ksapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func makeRSAKeypair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})
	return priv, string(pubPEM)
}

func signRS256(t *testing.T, priv *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "test-kid"
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestScopedJWTVerifierVerifyOK(t *testing.T) {
	priv, pubPEM := makeRSAKeypair(t)
	now := time.Now().Unix()
	token := signRS256(t, priv, jwt.MapClaims{
		"iss":            "keystone",
		"aud":            "ks.x.foo",
		"sub":            "u-1",
		"iat":            now,
		"exp":            now + 60,
		"kx_caller_id":   "ks-mcp-writer",
		"kx_caller_kind": "app",
		"kx_chain_id":    "chain-1",
		"kx_request_id":  "req-1",
	})
	v := NewScopedJWTVerifier("")
	if err := v.SetStaticKey("test-kid", pubPEM); err != nil {
		t.Fatal(err)
	}
	claims, err := v.Verify(token, "ks.x.foo")
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "u-1" {
		t.Fatalf("UserID = %q", claims.UserID)
	}
	if claims.CanonicalName != "ks.x.foo" {
		t.Fatalf("CanonicalName = %q", claims.CanonicalName)
	}
	if claims.CallerID != "ks-mcp-writer" {
		t.Fatalf("CallerID = %q", claims.CallerID)
	}
	if claims.CallerKind != "app" {
		t.Fatalf("CallerKind = %q", claims.CallerKind)
	}
	if claims.ChainID != "chain-1" {
		t.Fatalf("ChainID = %q", claims.ChainID)
	}
	if claims.RequestID != "req-1" {
		t.Fatalf("RequestID = %q", claims.RequestID)
	}
}

func TestScopedJWTVerifierWrongAud(t *testing.T) {
	priv, pubPEM := makeRSAKeypair(t)
	now := time.Now().Unix()
	token := signRS256(t, priv, jwt.MapClaims{
		"iss": "keystone", "aud": "ks.x.OTHER",
		"sub": "u", "iat": now, "exp": now + 60,
	})
	v := NewScopedJWTVerifier("")
	if err := v.SetStaticKey("test-kid", pubPEM); err != nil {
		t.Fatal(err)
	}
	_, err := v.Verify(token, "ks.x.foo")
	if !errors.Is(err, ErrTokenAudienceMismatch) {
		t.Fatalf("expected aud mismatch, got %v", err)
	}
}

func TestScopedJWTVerifierExpired(t *testing.T) {
	priv, pubPEM := makeRSAKeypair(t)
	now := time.Now().Unix()
	token := signRS256(t, priv, jwt.MapClaims{
		"iss": "keystone", "aud": "ks.x.foo",
		"sub": "u", "iat": now - 120, "exp": now - 60,
	})
	v := NewScopedJWTVerifier("")
	if err := v.SetStaticKey("test-kid", pubPEM); err != nil {
		t.Fatal(err)
	}
	_, err := v.Verify(token, "ks.x.foo")
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected token expired, got %v", err)
	}
}

func TestScopedJWTVerifierInvalidSignature(t *testing.T) {
	priv, _ := makeRSAKeypair(t)
	_, otherPub := makeRSAKeypair(t)
	now := time.Now().Unix()
	token := signRS256(t, priv, jwt.MapClaims{
		"iss": "keystone", "aud": "ks.x.foo",
		"sub": "u", "iat": now, "exp": now + 60,
	})
	v := NewScopedJWTVerifier("")
	if err := v.SetStaticKey("test-kid", otherPub); err != nil {
		t.Fatal(err)
	}
	_, err := v.Verify(token, "ks.x.foo")
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected token invalid, got %v", err)
	}
}

func TestScopedJWTVerifierUnknownKid(t *testing.T) {
	priv, _ := makeRSAKeypair(t)
	now := time.Now().Unix()
	token := signRS256(t, priv, jwt.MapClaims{
		"iss": "keystone", "aud": "ks.x.foo",
		"sub": "u", "iat": now, "exp": now + 60,
	})
	v := NewScopedJWTVerifier("")
	_, err := v.Verify(token, "ks.x.foo")
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected token invalid (unknown kid), got %v", err)
	}
}
