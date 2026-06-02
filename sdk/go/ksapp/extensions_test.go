package ksapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// TestHandle 验证自定义路由能通过 Handle 注册并正常响应。
func TestHandle(t *testing.T) {
	app := New("test")
	app.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"pong": "ok"})
	})

	handler := app.Mux()
	req := httptest.NewRequest("GET", "/api/ping", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["pong"] != "ok" {
		t.Errorf("pong = %q", resp["pong"])
	}
}

// TestHandle_StaticFiles 验证自定义路由可以挂载 http.FileServer。
func TestHandle_StaticFiles(t *testing.T) {
	app := New("test")
	// go.mod 在 sdk/go/（父目录），ksapp 包在 sdk/go/ksapp/
	app.Handle("GET /files/", http.StripPrefix("/files/", http.FileServer(http.Dir(".."))))

	handler := app.Mux()
	// 请求父目录下已知存在的文件
	req := httptest.NewRequest("GET", "/files/go.mod", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "module") {
		t.Error("expected go.mod content")
	}
}

// TestUse 验证中间件按注册顺序从外到内包装。
func TestUse(t *testing.T) {
	var order []string

	app := New("test")
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw1-after")
		})
	})
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw2-after")
		})
	})

	handler := app.Mux()
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	expected := []string{"mw1-before", "mw2-before", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("middleware order: %v, expected %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, expected %q", i, order[i], v)
		}
	}
}

// TestUse_AppliesToCustomRoutes 验证中间件也会应用到自定义路由。
func TestUse_AppliesToCustomRoutes(t *testing.T) {
	var called bool

	app := New("test")
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	})
	app.HandleFunc("GET /custom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	handler := app.Mux()
	req := httptest.NewRequest("GET", "/custom", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("middleware should apply to custom routes")
	}
}

// TestHealthCheck_AllPass 验证所有健康检查通过时 /healthz 返回 ok。
func TestHealthCheck_AllPass(t *testing.T) {
	app := New("test")
	app.HealthCheck("db", func() error { return nil })
	app.HealthCheck("cache", func() error { return nil })

	handler := app.Mux()
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q", resp["status"])
	}
}

// TestHealthCheck_OneFails 验证有健康检查失败时 /healthz 返回 503。
func TestHealthCheck_OneFails(t *testing.T) {
	app := New("test")
	app.HealthCheck("db", func() error { return nil })
	app.HealthCheck("disk", func() error { return fmt.Errorf("磁盘空间不足") })

	handler := app.Mux()
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Fatalf("status: %d, expected 503", w.Code)
	}
	var resp struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp.Status != "unhealthy" {
		t.Errorf("status = %q", resp.Status)
	}
	if resp.Checks["disk"] != "磁盘空间不足" {
		t.Errorf("disk check = %q", resp.Checks["disk"])
	}
	if _, has := resp.Checks["db"]; has {
		t.Error("passing check 'db' should not appear in failures")
	}
}

// TestHealthCheck_NoChecks 验证无自定义检查时 /healthz 仍返回 ok（向后兼容）。
func TestHealthCheck_NoChecks(t *testing.T) {
	app := New("test")

	handler := app.Mux()
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
}

// TestMux_BuiltinEndpointsStillWork 验证使用 Mux() 构建的 handler 包含所有内置端点。
func TestMux_BuiltinEndpointsStillWork(t *testing.T) {
	app := New("test")
	app.Tool("greet", "打招呼", func(ctx context.Context, params map[string]any) (any, error) {
		return map[string]string{"msg": "hello"}, nil
	})

	handler := app.Mux()

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/healthz"},
		{"GET", "/readyz"},
		{"GET", "/meta"},
		{"GET", "/mcp/tools/list"},
	}
	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("status: %d", w.Code)
			}
		})
	}
}

// TestMetaEndpoint_MatchesKSTypesSchema 验证 /meta 响应格式符合 kstypes.MetaResponse。
func TestMetaEndpoint_MatchesKSTypesSchema(t *testing.T) {
	t.Setenv("KEYSTONE_JWKS_URL", "http://not-reachable.test/jwks")
	app := New("ks-mcp-demo", WithKeystoneAuth(), WithVersion("2.3.4"))
	app.Tool("greet", "greet tool", func(ctx context.Context, p map[string]any) (any, error) {
		return "hi", nil
	})
	h := app.Mux()

	req := httptest.NewRequest("GET", "/meta", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var got kstypes.MetaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "ks-mcp-demo" {
		t.Errorf("name: got %q", got.Name)
	}
	if got.Version != "2.3.4" {
		t.Errorf("version: got %q", got.Version)
	}
	if got.AuthMode != kstypes.AuthModeKeystoneJWKS {
		t.Errorf("auth_mode: got %q", got.AuthMode)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "greet" {
		t.Errorf("tools: %+v", got.Tools)
	}
}

// TestFluentChaining 验证所有新增方法都支持链式调用。
func TestFluentChaining(t *testing.T) {
	app := New("test").
		Tool("t1", "desc", func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		}).
		HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) {}).
		Use(func(next http.Handler) http.Handler { return next }).
		HealthCheck("db", func() error { return nil })

	if app == nil {
		t.Fatal("chain returned nil")
	}
	if len(app.tools) != 1 {
		t.Errorf("tools: %d", len(app.tools))
	}
	if len(app.routes) != 1 {
		t.Errorf("routes: %d", len(app.routes))
	}
	if len(app.middlewares) != 1 {
		t.Errorf("middlewares: %d", len(app.middlewares))
	}
	if len(app.healthChecks) != 1 {
		t.Errorf("healthChecks: %d", len(app.healthChecks))
	}
}
