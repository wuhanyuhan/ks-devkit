// Package server 提供 conformance 套件用的 mock keystone /v1/apps/self/resources server。
// 响应格式按真实 keystone wire-level 协议：envelope {code, message, data: {app_id, version, install_id, env}}。
// claimant 把自己的 SelfClient 配到这个 server 的 URL，套件驱动各种场景验证。
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
)

// Scenario 控制 mock server 的响应行为。
type Scenario string

const (
	ScenarioOK             Scenario = "ok"               // 200 + envelope code=0 + 合法 env（全 string）
	ScenarioMixedTypes     Scenario = "mixed_types"      // 200 + env 含 number/bool 类型，验证 coerceString
	ScenarioUnauthorized   Scenario = "401"              // HTTP 401
	ScenarioServerError    Scenario = "500"              // HTTP 500
	ScenarioInvalidJSON    Scenario = "invalid_json"     // body 不是合法 JSON
	ScenarioBusinessError  Scenario = "business_error"   // 200 但 envelope.code != 0
	ScenarioMissingDataEnv Scenario = "missing_data_env" // 200 + envelope code=0 但 data.env 缺失
	ScenarioHang           Scenario = "hang"             // 永久挂起触发客户端超时
)

// New 启动 mock server。调用方测试结束应调 server.Close()。
// scenario 控制 server 行为；token 是 server 期望客户端发送的 Authorization Bearer 值。
func New(scenario Scenario, expectedToken string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验客户端发送的鉴权头
		if got := r.Header.Get("Authorization"); got != "Bearer "+expectedToken {
			http.Error(w, "unauthorized: bad bearer", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/apps/self/resources" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch scenario {
		case ScenarioOK:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"app_id":     "test-app",
					"version":    "1.0.0",
					"install_id": 42,
					"env": map[string]string{
						"DB_HOST":     "managed.example.com",
						"DB_PORT":     "3306",
						"DB_PASSWORD": "managed-secret",
					},
				},
			})
		case ScenarioMixedTypes:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"env": map[string]any{
						"DB_HOST": "x",
						"DB_PORT": 3306, // JSON number
						"DEBUG":   true, // JSON bool
					},
				},
			})
		case ScenarioUnauthorized:
			http.Error(w, "auth failed", http.StatusUnauthorized)
		case ScenarioServerError:
			http.Error(w, "internal error", http.StatusInternalServerError)
		case ScenarioInvalidJSON:
			_, _ = fmt.Fprint(w, "not json {{}}")
		case ScenarioBusinessError:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    10001,
				"message": "app not found",
				"data":    map[string]any{},
			})
		case ScenarioMissingDataEnv:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"app_id": "x"},
				// data.env 缺
			})
		case ScenarioHang:
			// 让客户端等到超时
			select {}
		default:
			http.Error(w, "unknown scenario", http.StatusInternalServerError)
		}
	}))
}
