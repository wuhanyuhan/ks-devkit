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

func newTestHandler(appID string, tools []ToolDef, callTool CallToolFunc) http.Handler {
	return NewStreamableHTTPHandler(appID, "0.1.0", tools, callTool)
}

func postMCP(handler http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestMCP_Initialize(t *testing.T) {
	handler := newTestHandler("test-app", nil, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Errorf("protocolVersion: %v", result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "test-app" {
		t.Errorf("serverInfo.name: %v", serverInfo["name"])
	}
}

func TestMCP_ToolsList(t *testing.T) {
	tools := []ToolDef{
		{Name: "greet", Description: "打招呼"},
	}
	handler := newTestHandler("test", tools, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)

	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp JSONRPCResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	result := resp.Result.(map[string]any)
	toolList := result["tools"].([]any)
	if len(toolList) != 1 {
		t.Fatalf("tools count: %d", len(toolList))
	}
	tool := toolList[0].(map[string]any)
	if tool["name"] != "greet" {
		t.Errorf("name: %v", tool["name"])
	}
	if tool["inputSchema"] == nil {
		t.Error("inputSchema 不应为 nil")
	}
}

func TestMCP_ToolsCall_Success(t *testing.T) {
	tools := []ToolDef{{Name: "add", Description: "加法"}}
	callTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		a := args["a"].(float64)
		b := args["b"].(float64)
		return map[string]float64{"sum": a + b}, nil
	}
	handler := newTestHandler("test", tools, callTool)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"add","arguments":{"a":1,"b":2}}}`)

	if w.Code != 200 {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
	}
	var resp JSONRPCResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	result := resp.Result.(map[string]any)
	content := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content 长度: %d", len(content))
	}
	item := content[0].(map[string]any)
	if item["type"] != "text" {
		t.Errorf("content type: %v", item["type"])
	}
	// 验证内容是 JSON 序列化的结果
	var innerResult map[string]float64
	if err := json.Unmarshal([]byte(item["text"].(string)), &innerResult); err != nil {
		t.Fatalf("解析 content text: %v", err)
	}
	if innerResult["sum"] != 3 {
		t.Errorf("sum: %v", innerResult["sum"])
	}
}

func TestMCP_ToolsCall_WithMeta(t *testing.T) {
	var capturedScope string
	tools := []ToolDef{{Name: "scoped", Description: "测试 scope"}}
	callTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		capturedScope = ResourceScope(ctx)
		return "ok", nil
	}
	handler := newTestHandler("test", tools, callTool)
	postMCP(handler, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"scoped","arguments":{},"_meta":{"ks_resource_scope":"instance_abc"}}}`)

	if capturedScope != "instance_abc" {
		t.Errorf("ResourceScope = %q，期望 instance_abc", capturedScope)
	}
}

func TestMCP_ToolsCall_NotFound(t *testing.T) {
	handler := newTestHandler("test", nil, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"missing","arguments":{}}}`)

	var resp JSONRPCError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("期望 error 响应")
	}
	if resp.Error.Code != errCodeInvalidParams {
		t.Errorf("error code: %d", resp.Error.Code)
	}
}

func TestMCP_MethodNotFound(t *testing.T) {
	handler := newTestHandler("test", nil, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":6,"method":"unknown/method","params":{}}`)

	var resp JSONRPCError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeMethodNotFound {
		t.Errorf("期望 MethodNotFound 错误")
	}
}

func TestMCP_InvalidJSON(t *testing.T) {
	handler := newTestHandler("test", nil, nil)
	w := postMCP(handler, `{invalid`)

	var resp JSONRPCError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeParseError {
		t.Errorf("期望 ParseError 错误")
	}
}

func TestMCP_ContentType(t *testing.T) {
	handler := newTestHandler("test", nil, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestResultToContent_String(t *testing.T) {
	content := resultToContent("hello world")
	if len(content) != 1 || content[0]["type"] != "text" || content[0]["text"] != "hello world" {
		t.Errorf("string content: %v", content)
	}
}

func TestResultToContent_Map(t *testing.T) {
	content := resultToContent(map[string]int{"a": 1})
	if len(content) != 1 || content[0]["type"] != "text" {
		t.Errorf("map content: %v", content)
	}
	// 验证是 JSON 字符串
	var m map[string]int
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &m); err != nil {
		t.Errorf("解析 JSON: %v", err)
	}
	if m["a"] != 1 {
		t.Errorf("a: %d", m["a"])
	}
}

// TestResultToContent_Nil 验证 nil 结果序列化为字符串 "null"，与 json.Marshal(nil) 行为一致。
func TestResultToContent_Nil(t *testing.T) {
	content := resultToContent(nil)
	if len(content) != 1 {
		t.Fatalf("content 长度: %d", len(content))
	}
	if content[0]["type"] != "text" {
		t.Errorf("content type: %v", content[0]["type"])
	}
	if content[0]["text"] != "null" {
		t.Errorf("content text = %v，期望 \"null\"", content[0]["text"])
	}
}

// TestMCP_Notification_NoResponse 验证 JSON-RPC 2.0 §4.1：
// 客户端发送无 id 字段的请求（notification），服务端应返回 202 且 body 为空。
// 这是 MCP 握手流程的关键 —— 客户端会发 notifications/initialized 通知，
// 之前的实现会误返回 MethodNotFound 响应导致握手失败。
func TestMCP_Notification_NoResponse(t *testing.T) {
	handler := newTestHandler("test", nil, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d，期望 202 Accepted", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("notification 不应有响应 body，实际: %s", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "" {
		t.Errorf("notification 不应设置 Content-Type，实际: %q", ct)
	}
}

// TestMCP_ToolsCall_HandlerError 验证 handler 返回的错误不会泄露原始文本到客户端。
// 见 streamable.go handleToolsCall：handler error 应通过 slog 记录，
// 客户端只收到固定提示 "工具执行失败"。
func TestMCP_ToolsCall_HandlerError(t *testing.T) {
	tools := []ToolDef{{Name: "leaky", Description: "返回包含敏感信息的错误"}}
	callTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		return nil, fmt.Errorf("数据库连接失败: mysql://user:pass@internal-host:3306")
	}
	handler := newTestHandler("test", tools, callTool)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"leaky","arguments":{}}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	var resp JSONRPCError
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("期望 error 响应")
	}
	if resp.Error.Code != errCodeInternal {
		t.Errorf("error code = %d，期望 %d", resp.Error.Code, errCodeInternal)
	}
	if resp.Error.Message != "工具执行失败" {
		t.Errorf("error message = %q，期望 \"工具执行失败\"", resp.Error.Message)
	}
	// 显式断言原始错误中的敏感片段没有泄露
	if strings.Contains(resp.Error.Message, "mysql://") {
		t.Errorf("error message 含敏感片段 mysql://: %q", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "internal-host") {
		t.Errorf("error message 含敏感片段 internal-host: %q", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "数据库连接失败") {
		t.Errorf("error message 含原始错误前缀: %q", resp.Error.Message)
	}
}

// TestMCP_ToolsCall_MissingParams 验证缺 params 字段时返回明确的错误消息。
func TestMCP_ToolsCall_MissingParams(t *testing.T) {
	handler := newTestHandler("test", nil, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":8,"method":"tools/call"}`)

	var resp JSONRPCError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeInvalidParams {
		t.Fatalf("期望 InvalidParams 错误，实际: %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "缺少 params") {
		t.Errorf("error message = %q，期望含 \"缺少 params\"", resp.Error.Message)
	}
}

// TestMCP_ToolsCall_MissingName 验证 params 中缺 name 字段时返回明确错误消息。
func TestMCP_ToolsCall_MissingName(t *testing.T) {
	handler := newTestHandler("test", nil, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"arguments":{}}}`)

	var resp JSONRPCError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeInvalidParams {
		t.Fatalf("期望 InvalidParams 错误，实际: %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "缺少 name") {
		t.Errorf("error message = %q，期望含 \"缺少 name\"", resp.Error.Message)
	}
}

// TestHandleToolsList_AnnotationsPassthrough 验证 ToolDef.Annotations 在
// tools/list 响应中按 MCP 2025-03-26 规范作为 "annotations" 字段透传。
// 字段缺省（nil/empty）时不应出现该 key（避免冗余）。
func TestHandleToolsList_AnnotationsPassthrough(t *testing.T) {
	t.Parallel()
	tools := []ToolDef{{
		Name:        "demo",
		Description: "demo tool",
		InputSchema: map[string]any{"type": "object"},
		Annotations: map[string]any{
			"readOnlyHint":  true,
			"openWorldHint": true,
		},
	}}
	handler := newTestHandler("test", tools, nil)
	w := postMCP(handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	result := resp.Result.(map[string]any)
	toolList := result["tools"].([]any)
	if len(toolList) != 1 {
		t.Fatalf("tools count: %d", len(toolList))
	}
	ann, ok := toolList[0].(map[string]any)["annotations"].(map[string]any)
	if !ok {
		t.Fatal("annotations 字段应出现")
	}
	if ann["readOnlyHint"] != true || ann["openWorldHint"] != true {
		t.Errorf("annotations payload mismatch: %v", ann)
	}
}
