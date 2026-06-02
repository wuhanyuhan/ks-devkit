package mcpproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacy_ToolsList(t *testing.T) {
	tools := []ToolDef{
		{Name: "greet", Description: "打招呼"},
		{Name: "add", Description: "加法"},
	}
	handler := NewLegacyListHandler(tools)
	req := httptest.NewRequest("GET", "/mcp/tools/list", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

func TestLegacy_ToolsList_Empty(t *testing.T) {
	handler := NewLegacyListHandler([]ToolDef{})
	req := httptest.NewRequest("GET", "/mcp/tools/list", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

func TestLegacy_ToolsCall_Success(t *testing.T) {
	tools := []ToolDef{{Name: "add", Description: "加法"}}
	callTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		a := args["a"].(float64)
		b := args["b"].(float64)
		return map[string]float64{"sum": a + b}, nil
	}
	handler := NewLegacyCallHandler(tools, callTool)
	body := strings.NewReader(`{"name":"add","params":{"a":1,"b":2}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

func TestLegacy_ToolsCall_NotFound(t *testing.T) {
	handler := NewLegacyCallHandler([]ToolDef{}, nil)
	body := strings.NewReader(`{"name":"missing","params":{}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status: %d", w.Code)
	}
}

func TestLegacy_ToolsCall_BadJSON(t *testing.T) {
	handler := NewLegacyCallHandler([]ToolDef{}, nil)
	body := strings.NewReader(`{invalid json`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status: %d", w.Code)
	}
}

// TestLegacy_ToolsCall_HandlerErrorSanitized 验证旧端点在 handler 返回 error 时
// 不会将原始错误文本泄露给客户端。完整错误通过 slog 记录到服务端日志，
// 客户端只收到固定提示 "工具执行失败"。
func TestLegacy_ToolsCall_HandlerErrorSanitized(t *testing.T) {
	tools := []ToolDef{{Name: "leaky", Description: "返回包含敏感信息的错误"}}
	callTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, fmt.Errorf("数据库连接失败: mysql://user:pass@internal-host:3306")
	}
	handler := NewLegacyCallHandler(tools, callTool)
	body := strings.NewReader(`{"name":"leaky","params":{}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

// TestLegacy_ToolsCall_ScopeFromMeta 验证 POST /mcp/tools/call 请求体中的 _meta 字段
// 能通过 withMeta 正确注入到 callTool 的 ctx，并且 _meta 本身不会泄露到 params 参数中。
func TestLegacy_ToolsCall_ScopeFromMeta(t *testing.T) {
	var capturedScope string
	var capturedParams map[string]any
	tools := []ToolDef{{Name: "scoped", Description: "scope 捕获"}}
	callTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		capturedScope = ResourceScope(ctx)
		capturedParams = args
		return map[string]string{"ok": "1"}, nil
	}
	handler := NewLegacyCallHandler(tools, callTool)
	body := strings.NewReader(`{"name":"scoped","params":{"_meta":{"ks_resource_scope":"inst_42"}}}`)
	req := httptest.NewRequest("POST", "/mcp/tools/call", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
	}
	if capturedScope != "inst_42" {
		t.Errorf("ResourceScope = %q, want inst_42", capturedScope)
	}
	if _, ok := capturedParams["_meta"]; ok {
		t.Error("_meta 不应泄露到 callTool 的 params 参数中")
	}
}
