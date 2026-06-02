package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// RequireConfigUIJWT 返回一个 net/http middleware，专门用于保护 MCP 配置 UI 的代理端点与 /config-* 端点。
// - 验 Bearer token 通过 JWKSVerifier 校验 RS256 签名
// - 断言 claims.type == "mcp_config_ui"
// - 断言 claims.mcp_server_id == os.Getenv("KSAPP_SERVER_ID")
// 失败分别返回 401（token 问题）/ 403（server_id 不匹配）/ 500（env 未配）。
func RequireConfigUIJWT(v *JWKSVerifier) func(http.Handler) http.Handler {
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

			// 断言 token 类型为 mcp_config_ui
			tokenType, _ := claims["type"].(string)
			if tokenType != "mcp_config_ui" {
				writeAuthError(w, "token 类型错误，期望 mcp_config_ui")
				return
			}

			// 读取服务端配置的 server id
			expectedServerID := os.Getenv("KSAPP_SERVER_ID")
			if expectedServerID == "" {
				writeJSONError(w, http.StatusInternalServerError, "KSAPP_SERVER_ID 环境变量未配置")
				return
			}

			// 兼容 claim 中 mcp_server_id 的 float64 / string 两种类型
			var claimServerID string
			switch tid := claims["mcp_server_id"].(type) {
			case float64:
				claimServerID = strconv.FormatInt(int64(tid), 10)
			case string:
				claimServerID = tid
			default:
				writeJSONError(w, http.StatusForbidden, "mcp_server_id 类型不支持")
				return
			}

			if claimServerID != expectedServerID {
				writeJSONError(w, http.StatusForbidden, "mcp_server_id 不匹配")
				return
			}

			ctx := context.WithValue(r.Context(), claimsCtxKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeJSONError 写入 JSON 错误响应（风格与 writeAuthError 保持一致）。
// 用于 RequireConfigUIJWT 中 401 之外的 403 / 500 场景。
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
