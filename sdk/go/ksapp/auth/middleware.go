package auth

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// claimsCtxKey 是 claims 在 request context 中的 key 类型。
// 用未导出的空结构体避免和其他 package 的 key 冲突。
type claimsCtxKey struct{}

// RequireJWT 返回一个 net/http middleware：
//   - 读取 Authorization: Bearer <jwt>
//   - 调用 JWKSVerifier 验证
//   - 验证成功：把 claims 注入 request context，调用下游 handler
//   - 任何失败：直接写 401 JSON {"error": "..."}
//
// 调用方通过 ClaimsFromContext(r.Context()) 读取 claims。
func RequireJWT(v *JWKSVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHdr := r.Header.Get("Authorization")
			if authHdr == "" {
				writeAuthError(w, "缺少 Authorization 头")
				return
			}
			parts := strings.SplitN(authHdr, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeAuthError(w, "Authorization 格式错误，期望 'Bearer <jwt>'")
				return
			}
			claims, err := v.Verify(parts[1])
			if err != nil {
				writeAuthError(w, "令牌验证失败")
				return
			}
			if !validateConfigUITokenServerID(w, claims) {
				return
			}
			ctx := context.WithValue(r.Context(), claimsCtxKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validateConfigUITokenServerID(w http.ResponseWriter, claims jwt.MapClaims) bool {
	tokenType, _ := claims["type"].(string)
	if tokenType != "mcp_config_ui" {
		return true
	}

	expectedServerID := os.Getenv("KSAPP_SERVER_ID")
	if expectedServerID == "" {
		writeJSONError(w, http.StatusInternalServerError, "KSAPP_SERVER_ID 环境变量未配置")
		return false
	}

	var claimServerID string
	switch tid := claims["mcp_server_id"].(type) {
	case float64:
		claimServerID = strconv.FormatInt(int64(tid), 10)
	case string:
		claimServerID = tid
	default:
		writeJSONError(w, http.StatusForbidden, "mcp_server_id 类型不支持")
		return false
	}

	if claimServerID != expectedServerID {
		writeJSONError(w, http.StatusForbidden, "mcp_server_id 不匹配")
		return false
	}
	return true
}

// ClaimsFromContext 从 request context 取 JWT claims。
// 当 RequireJWT 未启用或未放行时返回 nil, false。
func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	c, ok := ctx.Value(claimsCtxKey{}).(jwt.MapClaims)
	return c, ok
}

// writeAuthError 是 401 的便捷调用，等价于 writeJSONError(w, 401, msg)。
func writeAuthError(w http.ResponseWriter, msg string) {
	writeJSONError(w, http.StatusUnauthorized, msg)
}

// WithClaimsForTest 把 jwt.MapClaims 注入 ctx，与 RequireJWT 中间件写入的 key
// 完全一致；用于下游 SDK 用户（mcp 服务）对其 handler 做权限单元测试时构造
// 携带 claims 的 ctx。
//
// **仅供测试用**：生产代码请通过 RequireJWT 中间件流程注入 claims；
// 直接调用此函数会绕过 JWKS 验签，让任意 claims 都视作"合法"。
func WithClaimsForTest(parent context.Context, claims jwt.MapClaims) context.Context {
	return context.WithValue(parent, claimsCtxKey{}, claims)
}
