package ksapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/mcpproto"
	kstypes "github.com/wuhanyuhan/ks-types"
)

// newTestApp 构造一个注册了指定 tools 的 App，返回其 Mux() 以便 httptest 直接调用。
func newTestApp(appID string, tools []ToolDef) http.Handler {
	app := &App{
		id:        appID,
		toolNames: make(map[string]struct{}),
		tools:     tools,
	}
	for _, t := range tools {
		app.toolNames[t.Name] = struct{}{}
	}
	return app.Mux()
}

func TestHealthz(t *testing.T) {
	mux := newTestApp("test", nil)
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status: %q", resp["status"])
	}
}

func TestReadyz(t *testing.T) {
	mux := newTestApp("test", nil)
	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
}

func TestMeta(t *testing.T) {
	tools := []ToolDef{
		{Name: "greet", Description: "打招呼", Handler: nil},
		{Name: "add", Description: "加法", Handler: nil},
	}
	mux := newTestApp("my-app", tools)
	req := httptest.NewRequest("GET", "/meta", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp kstypes.MetaResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp.Name != "my-app" {
		t.Errorf("name: %q", resp.Name)
	}
	if len(resp.Tools) != 2 {
		t.Fatalf("tools count: %d", len(resp.Tools))
	}
	if resp.Tools[0].Name != "greet" || resp.Tools[0].Description != "打招呼" {
		t.Errorf("tools[0]: %+v", resp.Tools[0])
	}
}

func TestMCPToolCall_Success(t *testing.T) {
	tools := []ToolDef{
		{
			Name:        "add",
			Description: "加法",
			Handler: func(ctx context.Context, params map[string]any) (any, error) {
				a := params["a"].(float64)
				b := params["b"].(float64)
				return map[string]float64{"sum": a + b}, nil
			},
		},
	}
	mux := newTestApp("test", tools)
	body := strings.NewReader(`{"name":"add","params":{"a":1,"b":2}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["sum"].(float64) != 3.0 {
		t.Errorf("sum: %v", result["sum"])
	}
}

func TestMCPToolCall_NotFound(t *testing.T) {
	mux := newTestApp("test", []ToolDef{})
	body := strings.NewReader(`{"name":"missing","params":{}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status: %d", w.Code)
	}
}

func TestMCPToolCall_BadJSON(t *testing.T) {
	mux := newTestApp("test", []ToolDef{})
	body := strings.NewReader(`{invalid json`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("status: %d", w.Code)
	}
}

// TestLegacyMCPToolCall_HandlerErrorSanitized 验证旧 /mcp/tools/call 端点（过渡兼容）
// 在 handler 返回 error 时不会将原始错误文本泄露给客户端。与新 /mcp 端点行为一致：
// 完整错误通过 slog 记录到服务端日志，客户端只收到固定提示 "工具执行失败"。
func TestLegacyMCPToolCall_HandlerErrorSanitized(t *testing.T) {
	tools := []ToolDef{{
		Name:        "leaky",
		Description: "返回包含敏感信息的错误",
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			return nil, fmt.Errorf("数据库连接失败: mysql://user:pass@internal-host:3306")
		},
	}}
	protoDefs := []mcpproto.ToolDef{{Name: "leaky", Description: "返回包含敏感信息的错误"}}
	callTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		for _, t := range tools {
			if t.Name == name {
				return t.Handler(ctx, args)
			}
		}
		return nil, fmt.Errorf("工具 %q 未找到", name)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /mcp/tools/call", mcpproto.NewLegacyCallHandler(protoDefs, callTool))

	body := strings.NewReader(`{"name":"leaky","params":{}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d，期望 500", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	errMsg, ok := resp["error"].(string)
	if !ok {
		t.Fatalf("响应缺少 error 字段或类型不对: %+v", resp)
	}
	if errMsg != "工具执行失败" {
		t.Errorf("error = %q，期望 \"工具执行失败\"", errMsg)
	}
	// 显式断言原始错误中的敏感片段没有泄露到响应 body。
	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, "mysql://") {
		t.Errorf("响应 body 含敏感片段 mysql://: %s", bodyStr)
	}
	if strings.Contains(bodyStr, "internal-host") {
		t.Errorf("响应 body 含敏感片段 internal-host: %s", bodyStr)
	}
	if strings.Contains(bodyStr, "数据库连接失败") {
		t.Errorf("响应 body 含原始错误前缀: %s", bodyStr)
	}
}

func TestMCPToolsList(t *testing.T) {
	tools := []ToolDef{
		{Name: "greet", Description: "打招呼", Handler: nil},
		{Name: "add", Description: "加法", Handler: nil},
	}
	mux := newTestApp("test", tools)
	req := httptest.NewRequest("GET", "/mcp/tools/list", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(resp.Tools) != 2 {
		t.Fatalf("tools count: %d", len(resp.Tools))
	}
	if resp.Tools[0].Name != "greet" || resp.Tools[1].Name != "add" {
		t.Errorf("unexpected tools: %+v", resp.Tools)
	}
}

func TestMCPToolsList_Empty(t *testing.T) {
	mux := newTestApp("test", []ToolDef{})
	req := httptest.NewRequest("GET", "/mcp/tools/list", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp struct {
		Tools []any `json:"tools"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(resp.Tools) != 0 {
		t.Errorf("expected empty tools, got %d", len(resp.Tools))
	}
}

// TestMCPToolCall_ScopeFromMeta 验证 POST /mcp/tools/call 请求体中的 _meta 字段
// 能通过 withMeta 正确注入到 handler 的 ctx，并且 _meta 本身不会泄露到 handler 的
// params 参数中。防止 registerMCPEndpoint 重构时 _meta 注入链路静默退化。
func TestMCPToolCall_ScopeFromMeta(t *testing.T) {
	var capturedScope string
	var metaLeaked bool
	tools := []ToolDef{{
		Name:        "scoped",
		Description: "scope 捕获",
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			capturedScope = ResourceScope(ctx)
			if _, ok := params["_meta"]; ok {
				metaLeaked = true
			}
			return map[string]string{"ok": "1"}, nil
		},
	}}
	mux := newTestApp("test", tools)
	body := strings.NewReader(`{"name":"scoped","params":{"_meta":{"ks_resource_scope":"inst_42"}}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
	}
	if capturedScope != "inst_42" {
		t.Errorf("ResourceScope = %q, want inst_42", capturedScope)
	}
	if metaLeaked {
		t.Error("_meta 不应泄露到 handler 的 params 参数中")
	}
}

// TestToolWithSchema 验证 ToolWithSchema 正确写入 InputSchema 并复用 Tool 的注册逻辑。
func TestToolWithSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number"},
			"b": map[string]any{"type": "number"},
		},
		"required": []string{"a", "b"},
	}
	a := New("schema-app").ToolWithSchema("add", "加法", schema,
		func(ctx context.Context, params map[string]any) (any, error) {
			return nil, nil
		})

	if len(a.tools) != 1 {
		t.Fatalf("tools 数量: %d", len(a.tools))
	}
	tool := a.tools[0]
	if tool.Name != "add" {
		t.Errorf("tool name: %q", tool.Name)
	}
	if tool.Description != "加法" {
		t.Errorf("tool description: %q", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Fatal("InputSchema 不应为 nil")
	}
	if tool.InputSchema["type"] != "object" {
		t.Errorf("InputSchema type: %v", tool.InputSchema["type"])
	}
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema properties 类型不对: %T", tool.InputSchema["properties"])
	}
	if _, hasA := props["a"]; !hasA {
		t.Error("InputSchema.properties 缺少 a")
	}

	// 验证重复注册检测仍然生效（ToolWithSchema 应复用 Tool 的逻辑）
	defer func() {
		if r := recover(); r == nil {
			t.Error("期望重复注册时 panic")
		}
	}()
	a.ToolWithSchema("add", "重复", schema, func(ctx context.Context, params map[string]any) (any, error) {
		return nil, nil
	})
}

// TestContentTypeJSON 验证所有 JSON 端点均返回 application/json Content-Type，与 Python SDK 保持一致。
func TestContentTypeJSON(t *testing.T) {
	tools := []ToolDef{
		{
			Name:        "ping",
			Description: "测试",
			Handler: func(ctx context.Context, params map[string]any) (any, error) {
				return map[string]string{"pong": "ok"}, nil
			},
		},
	}
	mux := newTestApp("ct-test", tools)

	cases := []struct {
		name           string
		method         string
		path           string
		body           string
		expectedStatus int
	}{
		{"healthz", "GET", "/healthz", "", 200},
		{"readyz", "GET", "/readyz", "", 200},
		{"meta", "GET", "/meta", "", 200},
		{"mcp tools list", "GET", "/mcp/tools/list", "", 200},
		{"mcp success", "POST", "/mcp/tools/call", `{"name":"ping","params":{}}`, 200},
		{"mcp bad json", "POST", "/mcp/tools/call", `{invalid`, 400},
		{"mcp not found", "POST", "/mcp/tools/call", `{"name":"missing","params":{}}`, 404},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != c.expectedStatus {
				t.Errorf("status = %d, 期望 %d", w.Code, c.expectedStatus)
			}
			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q，期望 application/json", ct)
			}
		})
	}
}

func TestMux_MCPRouteProtectedByAuth(t *testing.T) {
	// 使用 testJWKSServer 能力需要跨包；这里用一个简单的 negative test：
	// 无 Authorization 的 POST /mcp 应 401
	t.Setenv("KEYSTONE_JWKS_URL", "http://not-reachable.test/jwks")
	app := New("demo", WithKeystoneAuth())
	app.Tool("noop", "noop", func(ctx context.Context, p map[string]any) (any, error) {
		return nil, nil
	})
	h := app.Mux()

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("未鉴权 POST /mcp 应 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMux_HealthzNotProtected(t *testing.T) {
	t.Setenv("KEYSTONE_JWKS_URL", "http://not-reachable.test/jwks")
	app := New("demo", WithKeystoneAuth())
	h := app.Mux()

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("/healthz 不应被 auth 保护, got %d", rec.Code)
	}
}

func TestMux_MetaNotProtected(t *testing.T) {
	t.Setenv("KEYSTONE_JWKS_URL", "http://not-reachable.test/jwks")
	app := New("demo", WithKeystoneAuth())
	h := app.Mux()

	req := httptest.NewRequest("GET", "/meta", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("/meta 不应被 auth 保护, got %d", rec.Code)
	}
}

func TestMux_NoAuthWhenAuthModeNone(t *testing.T) {
	app := New("demo") // 默认 none
	app.Tool("noop", "noop", func(ctx context.Context, p map[string]any) (any, error) {
		return "ok", nil
	})
	h := app.Mux()

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == 401 {
		t.Error("authMode=none 时 /mcp 不应要求鉴权")
	}
}
