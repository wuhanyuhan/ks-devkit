// Package auth 提供 Keystone 生态的 JWT/JWKS 验证能力。
//
// 实现采用 net/http 中立风格，供 ksapp App 的 middleware 挂载使用。
package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWKSVerifier 通过 JWKS 端点拉取 RSA 公钥验证 RS256 JWT。
// 缓存所有 key 1 小时，过期后按需重拉。
type JWKSVerifier struct {
	jwksURL    string
	cache      map[string]*rsa.PublicKey
	cacheTime  time.Time
	cacheTTL   time.Duration
	mu         sync.Mutex
	httpClient *http.Client
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Use string `json:"use"`
	Alg string `json:"alg"`
}

// NewJWKSVerifier 创建一个 JWKSVerifier。jwksURL 为空则任何 Verify 都会返回错误。
func NewJWKSVerifier(jwksURL string) *JWKSVerifier {
	return &JWKSVerifier{
		jwksURL:    jwksURL,
		cache:      make(map[string]*rsa.PublicKey),
		cacheTTL:   time.Hour,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// URL 返回 verifier 配置的 JWKS URL（空串表示未配置）。
func (v *JWKSVerifier) URL() string { return v.jwksURL }

// Verify 解析并验证 JWT 字符串。仅接受 RS256 算法的 token。
// 返回 claims 或错误（过期、签名错、未知 kid 等）。
func (v *JWKSVerifier) Verify(tokenStr string) (jwt.MapClaims, error) {
	if v.jwksURL == "" {
		return nil, fmt.Errorf("JWKS URL 未配置，无法验证")
	}
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("不支持的签名算法: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("JWT header 缺少 kid")
		}
		return v.getKey(kid)
	})
	if err != nil {
		return nil, fmt.Errorf("JWT 验证失败: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("无效的 JWT claims")
	}
	return claims, nil
}

func (v *JWKSVerifier) getKey(kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Since(v.cacheTime) < v.cacheTTL {
		if k, ok := v.cache[kid]; ok {
			return k, nil
		}
	}
	if err := v.fetch(); err != nil {
		return nil, fmt.Errorf("获取 JWKS 失败: %w", err)
	}
	k, ok := v.cache[kid]
	if !ok {
		return nil, fmt.Errorf("未找到 kid=%s 对应的公钥", kid)
	}
	return k, nil
}

func (v *JWKSVerifier) fetch() error {
	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("请求 JWKS 端点失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS 端点返回非 200: %d", resp.StatusCode)
	}
	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("解析 JWKS 响应失败: %w", err)
	}
	newCache := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if !strings.EqualFold(key.Kty, "RSA") {
			continue
		}
		pub, err := parseRSAPublicKey(key.N, key.E)
		if err != nil {
			continue
		}
		newCache[key.Kid] = pub
	}
	v.cache = newCache
	v.cacheTime = time.Now()
	return nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("解码 n 失败: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("解码 e 失败: %w", err)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}
