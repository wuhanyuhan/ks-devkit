package ksapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// scopedClaimsCtxKey 是 *ScopedClaims 在 request context 中的 key 类型。
// 用未导出空结构体避免外部 package 误用；scoped JWT 路径下 capability handler
// 读取由 makeHTTPCapabilityHandler 走内部 helper 提取。
type scopedClaimsCtxKey struct{}

// scopedClaimsFromRequest 从 request context 取 *ScopedClaims（http_endpoint handler 内部用）。
func scopedClaimsFromRequest(r *http.Request) (*ScopedClaims, bool) {
	v := r.Context().Value(scopedClaimsCtxKey{})
	if v == nil {
		return nil, false
	}
	c, ok := v.(*ScopedClaims)
	return c, ok
}

// ScopedJWTMiddleware 保护 http_endpoint capability path。
//
// 行为：
//   - 路径不在 pathToCanonicalName：pass-through（非 capability 路径不受此 middleware 影响）
//   - Authorization 头不含 Bearer：401 missing_bearer
//   - 验签失败：401 token_invalid / token_expired / aud_mismatch（按 sentinel 区分）
//   - 验签成功：把 *ScopedClaims 注入 request context，调下游
func ScopedJWTMiddleware(
	verifier *ScopedJWTVerifier,
	pathToCanonicalName map[string]string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expectedAud, ok := pathToCanonicalName[r.URL.Path]
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				writeScopedJWTError(w, http.StatusUnauthorized, "missing_bearer", "missing Bearer authorization header")
				return
			}
			token := strings.TrimSpace(authHeader[len("Bearer "):])
			claims, err := verifier.Verify(token, expectedAud)
			if err != nil {
				switch {
				case errors.Is(err, ErrTokenAudienceMismatch):
					writeScopedJWTError(w, http.StatusUnauthorized, "aud_mismatch", err.Error())
				case errors.Is(err, ErrTokenExpired):
					writeScopedJWTError(w, http.StatusUnauthorized, "token_expired", err.Error())
				default:
					writeScopedJWTError(w, http.StatusUnauthorized, "token_invalid", err.Error())
				}
				return
			}
			ctx := context.WithValue(r.Context(), scopedClaimsCtxKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeScopedJWTError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   code,
		"message": message,
		"code":    status,
	})
}
