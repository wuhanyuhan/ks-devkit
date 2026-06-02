package ksapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// ScopedClaims 是 scoped JWT 解码后的 claims 子集。
// dispatcher 调 http_endpoint backend 时签发，aud=canonical_name，承载 caller 身份 +
// chain trace。
type ScopedClaims struct {
	UserID        string // sub
	CanonicalName string // aud
	CallerID      string // kx_caller_id
	CallerKind    string // kx_caller_kind
	ChainID       string // kx_chain_id
	RequestID     string // kx_request_id
	IssuedAt      int64  // iat
	ExpiresAt     int64  // exp
}

// ScopedJWTVerifier 验证 scoped JWT（http_endpoint backend 用）。
//
// 设计：
//   - 内部 staticKeys（kid → RSA public key），供测试与小规模部署用
//   - jwksURL 字段预留 JWKS fetch 入口（当前暂未实现 JWKS lazy 拉取，
//     生产可在启动期通过 SetStaticKey 注入；后续接入 JWKS）
type ScopedJWTVerifier struct {
	jwksURL    string
	staticKeys map[string]*rsa.PublicKey
}

func NewScopedJWTVerifier(jwksURL string) *ScopedJWTVerifier {
	return &ScopedJWTVerifier{
		jwksURL:    jwksURL,
		staticKeys: make(map[string]*rsa.PublicKey),
	}
}

// SetStaticKey 注入 kid → RSA public key（PEM 编码）。
// 主要给单测用；生产部署也可在启动期注入受信的 keystone 公钥。
func (v *ScopedJWTVerifier) SetStaticKey(kid, pubPEM string) error {
	block, _ := pem.Decode([]byte(pubPEM))
	if block == nil {
		return errors.New("invalid PEM")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return errors.New("not an RSA public key")
	}
	v.staticKeys[kid] = pub
	return nil
}

// Verify 验签 + aud 校验，返回 ScopedClaims。
//
// 错误映射：
//   - aud mismatch → ErrTokenAudienceMismatch（携带 expected）
//   - exp 已过 → ErrTokenExpired
//   - 签名 / 格式 / kid 未知等 → ErrTokenInvalid
func (v *ScopedJWTVerifier) Verify(tokenStr, expectedAud string) (*ScopedClaims, error) {
	parsed, err := jwt.Parse(tokenStr, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrTokenInvalid, tok.Header["alg"])
		}
		kid, _ := tok.Header["kid"].(string)
		if key, ok := v.staticKeys[kid]; ok {
			return key, nil
		}
		return nil, fmt.Errorf("%w: unknown kid %q", ErrTokenInvalid, kid)
	}, jwt.WithAudience(expectedAud))
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, fmt.Errorf("%w: %v", ErrTokenExpired, err)
		case errors.Is(err, jwt.ErrTokenInvalidAudience):
			return nil, NewTokenAudienceMismatch(fmt.Sprintf("expected=%s err=%v", expectedAud, err))
		default:
			return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
		}
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected claims type", ErrTokenInvalid)
	}
	return &ScopedClaims{
		UserID:        asString(mc["sub"]),
		CanonicalName: asString(mc["aud"]),
		CallerID:      asString(mc["kx_caller_id"]),
		CallerKind:    asString(mc["kx_caller_kind"]),
		ChainID:       asString(mc["kx_chain_id"]),
		RequestID:     asString(mc["kx_request_id"]),
		IssuedAt:      asInt64(mc["iat"]),
		ExpiresAt:     asInt64(mc["exp"]),
	}, nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	default:
		return 0
	}
}
