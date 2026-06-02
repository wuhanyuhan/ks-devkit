package ksapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestNewLLMClient_ReadsEnv 构造器从环境变量读取 gateway URL 和 token。
func TestNewLLMClient_ReadsEnv(t *testing.T) {
	t.Setenv("KS_GATEWAY_URL", "http://test-gateway:9988")
	t.Setenv("KS_RELAY_TOKEN", "token-abc")

	c := newLLMClient()
	if c.gatewayURL != "http://test-gateway:9988" {
		t.Errorf("gatewayURL: %q", c.gatewayURL)
	}
	if c.relayToken != "token-abc" {
		t.Errorf("relayToken: %q", c.relayToken)
	}
}

// TestNewLLMClient_DefaultGatewayURL KS_GATEWAY_URL 未设置时使用默认值。
func TestNewLLMClient_DefaultGatewayURL(t *testing.T) {
	t.Setenv("KS_GATEWAY_URL", "")
	t.Setenv("KS_RELAY_TOKEN", "token")

	c := newLLMClient()
	if c.gatewayURL != "http://localhost:9988" {
		t.Errorf("默认 gatewayURL: %q", c.gatewayURL)
	}
}

// TestNewLLMClient_TrimsTrailingSlash 网关 URL 末尾的 / 应被 strip，
// 避免拼接 path 时产生 http://host//v1/... 这种双斜杠 URL。
func TestNewLLMClient_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("KS_GATEWAY_URL", "http://host:9988/")
	t.Setenv("KS_RELAY_TOKEN", "t")
	c := newLLMClient()
	if c.gatewayURL != "http://host:9988" {
		t.Errorf("应 trim 末尾 /: %q", c.gatewayURL)
	}
}

// TestNewLLMClient_KeystoneRelayTokenFallback KS_RELAY_TOKEN 为空时回落 KEYSTONE_RELAY_TOKEN
// （keystone 平台安装时注入名），对齐 python 侧。
func TestNewLLMClient_KeystoneRelayTokenFallback(t *testing.T) {
	t.Setenv("KS_RELAY_TOKEN", "")
	t.Setenv("KEYSTONE_RELAY_TOKEN", "keystone-injected")
	if c := newLLMClient(); c.relayToken != "keystone-injected" {
		t.Errorf("应回落 KEYSTONE_RELAY_TOKEN，得 %q", c.relayToken)
	}
}

// TestNewLLMClient_KSRelayTokenWins 两个 env 都设时 KS_RELAY_TOKEN 优先。
func TestNewLLMClient_KSRelayTokenWins(t *testing.T) {
	t.Setenv("KS_RELAY_TOKEN", "primary")
	t.Setenv("KEYSTONE_RELAY_TOKEN", "secondary")
	if c := newLLMClient(); c.relayToken != "primary" {
		t.Errorf("两者都设时 KS_RELAY_TOKEN 优先，得 %q", c.relayToken)
	}
}

// TestLLMClient_ChatNoToken 未配置 token 时，Chat 立即返回 ErrNotConfigured。
func TestLLMClient_ChatNoToken(t *testing.T) {
	c := &LLMClient{gatewayURL: "http://x", relayToken: ""}
	_, err := c.Chat(nil, ChatRequest{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("期望 ErrNotConfigured，得到: %v", err)
	}
}

// TestLLMClient_StreamChatNoToken 未配置 token 时，StreamChat 立即返回 ErrNotConfigured。
func TestLLMClient_StreamChatNoToken(t *testing.T) {
	c := &LLMClient{gatewayURL: "http://x", relayToken: ""}
	err := c.StreamChat(nil, ChatRequest{}, func(Chunk) {})
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("期望 ErrNotConfigured，得到: %v", err)
	}
}

// TestChatRequest_JSONSerialize 校验 ChatRequest 序列化的 JSON tag。
func TestChatRequest_JSONSerialize(t *testing.T) {
	temp := 0.7
	req := ChatRequest{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: &temp,
		MaxTokens:   100,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)
	// 字段名用 snake_case
	if !strings.Contains(s, `"model":"gpt-4o"`) {
		t.Errorf("model 字段: %s", s)
	}
	if !strings.Contains(s, `"messages":[`) {
		t.Errorf("messages 字段: %s", s)
	}
	if !strings.Contains(s, `"temperature":0.7`) {
		t.Errorf("temperature 字段: %s", s)
	}
	if !strings.Contains(s, `"max_tokens":100`) {
		t.Errorf("max_tokens 字段: %s", s)
	}
}

// TestChatRequest_JSONOmitEmpty 未设置字段应被 omitempty 省略。
func TestChatRequest_JSONOmitEmpty(t *testing.T) {
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
	data, _ := json.Marshal(req)
	s := string(data)
	if strings.Contains(s, "model") {
		t.Errorf("model 应省略: %s", s)
	}
	if strings.Contains(s, "temperature") {
		t.Errorf("temperature 应省略: %s", s)
	}
	if strings.Contains(s, "max_tokens") {
		t.Errorf("max_tokens 应省略: %s", s)
	}
	// messages 是必填字段，无 omitempty，即使为空也必须存在
	if !strings.Contains(s, `"messages"`) {
		t.Errorf("messages 应始终存在: %s", s)
	}
}

// TestChatRequest_IntentFoldsIntoRequestOptions intent 一等字段（Tier/Reasoning/
// RequireCapabilities）经 MarshalJSON 折叠进 request_options，且不作顶层 wire 字段。
func TestChatRequest_IntentFoldsIntoRequestOptions(t *testing.T) {
	req := ChatRequest{
		Messages:            []Message{{Role: "user", Content: "hi"}},
		Tier:                "flagship",
		Reasoning:           "on",
		RequireCapabilities: []string{"vision"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	opts, ok := got["request_options"].(map[string]any)
	if !ok {
		t.Fatalf("request_options 缺失或类型错误: %s", data)
	}
	if opts["tier"] != "flagship" || opts["reasoning_mode"] != "on" || opts["vision_required"] != true {
		t.Errorf("intent 折叠错误: %v", opts)
	}
	// intent 字段本身不应作为顶层 wire 字段出现
	if _, exists := got["tier"]; exists {
		t.Errorf("tier 不应是顶层字段: %s", data)
	}
	if _, exists := got["RequireCapabilities"]; exists {
		t.Errorf("RequireCapabilities 不应序列化: %s", data)
	}
}

func TestChatRequest_NoIntent_OmitsRequestOptions(t *testing.T) {
	req := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	data, _ := json.Marshal(req)
	if strings.Contains(string(data), "request_options") {
		t.Errorf("无 intent 时不应出现 request_options: %s", data)
	}
}

func TestChatRequest_MergesExistingRequestOptions(t *testing.T) {
	req := ChatRequest{
		Messages:       []Message{{Role: "user", Content: "hi"}},
		RequestOptions: map[string]interface{}{"foo": "bar"},
		Tier:           "economy",
	}
	data, _ := json.Marshal(req)
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	opts := got["request_options"].(map[string]any)
	if opts["foo"] != "bar" || opts["tier"] != "economy" {
		t.Errorf("应合并既有 request_options: %v", opts)
	}
}

// TestApp_LLM_Singleton App.LLM() 在 New 中预初始化，多次调用返回同一实例。
func TestApp_LLM_Singleton(t *testing.T) {
	app := New("test-app")
	if app.llm == nil {
		t.Fatal("New 后 llm 字段应已预初始化")
	}
	c1 := app.LLM()
	c2 := app.LLM()
	if c1 != c2 {
		t.Errorf("LLM() 应返回同一实例")
	}
}

// TestChat_SuccessAndURL 校验 URL、鉴权头、请求体、响应解析。
func TestChat_SuccessAndURL(t *testing.T) {
	var capturedPath, capturedAuth, capturedCT string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "chat.completion",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "你好！"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
		}`))
	}))
	defer srv.Close()

	c := &LLMClient{gatewayURL: srv.URL, relayToken: "test-token", httpClient: &http.Client{}}
	temp := 0.5
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: "user", Content: "你好"}},
		Temperature: &temp,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// URL 必须是 /v1/mcp/relay/chat/completions（修复原 bug）
	if capturedPath != "/v1/mcp/relay/chat/completions" {
		t.Errorf("path: %q", capturedPath)
	}
	if capturedAuth != "Bearer test-token" {
		t.Errorf("auth: %q", capturedAuth)
	}
	if capturedCT != "application/json" {
		t.Errorf("content-type: %q", capturedCT)
	}
	// 请求体字段
	if capturedBody["model"] != "gpt-4o" {
		t.Errorf("model: %v", capturedBody["model"])
	}
	if capturedBody["temperature"] != 0.5 {
		t.Errorf("temperature: %v", capturedBody["temperature"])
	}
	if capturedBody["max_tokens"] != float64(100) {
		t.Errorf("max_tokens: %v", capturedBody["max_tokens"])
	}

	// 响应字段
	if resp.Content != "你好！" {
		t.Errorf("content: %q", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finish_reason: %q", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("total_tokens: %d", resp.Usage.TotalTokens)
	}
}

// TestChat_ToolCallsResponse 响应中含 tool_calls 时正确解析。
func TestChat_ToolCallsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "chat.completion",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {"name": "get_weather", "arguments": "{\"city\":\"北京\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer srv.Close()

	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	resp, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "北京天气"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool_calls 数量: %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "get_weather" {
		t.Errorf("tool_call: %+v", tc)
	}
	if !strings.Contains(tc.Function.Arguments, "北京") {
		t.Errorf("tool_call arguments: %q", tc.Function.Arguments)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish_reason: %q", resp.FinishReason)
	}
}

// TestChat_Unauthorized 401/403 → ErrUnauthorized。
func TestChat_Unauthorized(t *testing.T) {
	for _, code := range []int{401, 403} {
		t.Run("", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"error":"bad token"}`))
			}))
			defer srv.Close()
			c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
			_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
			if !errors.Is(err, ErrUnauthorized) {
				t.Errorf("code %d 期望 ErrUnauthorized，得到: %v", code, err)
			}
		})
	}
}

// TestChat_RateLimited 429 → ErrRateLimited。
func TestChat_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("期望 ErrRateLimited，得到: %v", err)
	}
}

// TestChat_Upstream 5xx → ErrUpstream，错误信息包含状态码和 body。
func TestChat_Upstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("upstream timeout"))
	}))
	defer srv.Close()
	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("期望 ErrUpstream，得到: %v", err)
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "upstream timeout") {
		t.Errorf("错误信息缺少状态码/body: %v", err)
	}
}

// TestChat_InvalidJSON 200 响应体非合法 JSON 时报解析错误。
func TestChat_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not a json"))
	}))
	defer srv.Close()
	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("期望解析错误")
	}
	if !strings.Contains(err.Error(), "解析") {
		t.Errorf("错误信息应含『解析』: %v", err)
	}
}

// TestChat_ContextCancel ctx 取消时返回 context.Canceled。
func TestChat_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	_, err := c.Chat(ctx, ChatRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("期望 context 错误")
	}
}

// TestChat_ForcesStreamFalse 调用方误传 Stream: true 时，Chat 应强制把请求体里的 stream 置 false。
// 由于 ChatRequest.Stream 有 omitempty，false 零值会被省略 —— 这个断言同时验证了 omitempty 行为。
func TestChat_ForcesStreamFalse(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[],"usage":{}}`))
	}))
	defer srv.Close()

	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	_, _ = c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
		Stream:   true, // 调用方错传
	})

	if _, ok := body["stream"]; ok {
		t.Errorf("Chat 应强制 stream=false 并被 omitempty 省略，但 body 含 stream: %v", body)
	}
}

// TestStreamChat_TextOnlyFixture 用 fixture 01 回归：纯文本流 → 3 个 Chunk。
func TestStreamChat_TextOnlyFixture(t *testing.T) {
	assertStreamFixture(t, "01-text-only")
}

// TestStreamChat_ToolCallsFixture 用 fixture 02 回归：工具调用流。
func TestStreamChat_ToolCallsFixture(t *testing.T) {
	assertStreamFixture(t, "02-tool-calls")
}

// TestStreamChat_WithUsageFixture 用 fixture 03 回归：最后 chunk 带 usage。
func TestStreamChat_WithUsageFixture(t *testing.T) {
	assertStreamFixture(t, "03-with-usage")
}

// assertStreamFixture 公共测试辅助：从 shared-fixtures 读原始 SSE 和预期 chunks，
// 启动 mock server 返回该 SSE，跑 StreamChat，比对解析结果与预期一致。
func assertStreamFixture(t *testing.T, name string) {
	t.Helper()

	// shared-fixtures 相对 sdk/go/ksapp/ 的路径：../../shared-fixtures/sse/
	// 文件命名规则：NN-<topic>.sse 对应 NN-expected-chunks.json（共享 README）
	fixtureDir := filepath.Join("..", "..", "shared-fixtures", "sse")
	ssePath := filepath.Join(fixtureDir, name+".sse")
	prefix := name
	if idx := strings.Index(name, "-"); idx > 0 {
		prefix = name[:idx]
	}
	expectedPath := filepath.Join(fixtureDir, prefix+"-expected-chunks.json")

	sseBytes, err := os.ReadFile(ssePath)
	if err != nil {
		t.Fatalf("读 SSE fixture: %v", err)
	}
	expectedBytes, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("读 expected fixture: %v", err)
	}
	var expected []map[string]any
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("parse expected: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(200)
		_, _ = w.Write(sseBytes)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}

	var (
		mu     sync.Mutex
		chunks []Chunk
	)
	err = c.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, func(ch Chunk) {
		mu.Lock()
		chunks = append(chunks, ch)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	if len(chunks) != len(expected) {
		t.Fatalf("chunk 数量：实际 %d，预期 %d", len(chunks), len(expected))
	}

	// 比对：每个 chunk 序列化后应等价于 expected
	for i, ch := range chunks {
		got, _ := json.Marshal(ch)
		gotMap := map[string]any{}
		_ = json.Unmarshal(got, &gotMap)

		if !chunkMatchesExpected(gotMap, expected[i]) {
			t.Errorf("chunk[%d] 不匹配\n got: %v\nwant: %v", i, gotMap, expected[i])
		}
	}
}

// chunkMatchesExpected 按"结构等价"比对两个 chunk map，允许三端序列化细节差异：
//   - want 里存在且非零值的字段，got 里必须存在且值等价（递归进入 map/array）
//   - want 里字段值是零值（空串/空数组/空对象/null）时，got 可省略该字段或也为零值
//   - got 里额外的零值字段允许
//
// 这是因为 Go 的 omitempty 无法表达"零值字段必须保留"，而 Python/TS 能，
// 但三端拿到的内存数据字段值必须一致——fixture 只验证语义等价，不验证 JSON 逐字节一致。
func chunkMatchesExpected(got, want map[string]any) bool {
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			// got 缺该 key：仅当 want 值本身是零值时视为匹配
			if isZeroJSONValue(wv) {
				continue
			}
			return false
		}
		if !valueMatches(gv, wv) {
			return false
		}
	}
	return true
}

// valueMatches 递归比对两个 JSON 解码后的值。
func valueMatches(got, want any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		return chunkMatchesExpected(g, w)
	case []any:
		g, ok := got.([]any)
		if !ok {
			return false
		}
		if len(g) != len(w) {
			return false
		}
		for i := range w {
			if !valueMatches(g[i], w[i]) {
				return false
			}
		}
		return true
	default:
		wb, _ := json.Marshal(want)
		gb, _ := json.Marshal(got)
		return string(wb) == string(gb)
	}
}

// isZeroJSONValue 判断一个 JSON 解码后的值是否属于"零值"语义：
// nil、空串、数字 0、false、空数组、空对象。
func isZeroJSONValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case float64:
		return x == 0
	case bool:
		return !x
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// TestStreamChat_ErrorStatus 流式请求得到非 200 时返回错误。
func TestStreamChat_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	err := c.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
	}, func(Chunk) {})
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("期望 ErrRateLimited: %v", err)
	}
}

// TestStreamChat_ForcesStreamTrue 校验 StreamChat 强制把 req.Stream 设为 true。
func TestStreamChat_ForcesStreamTrue(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := &LLMClient{gatewayURL: srv.URL, relayToken: "t", httpClient: &http.Client{}}
	_ = c.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "x"}},
		Stream:   false, // 用户传 false，SDK 必须覆盖
	}, func(Chunk) {})

	if body["stream"] != true {
		t.Errorf("SDK 应强制 stream=true，实际: %v", body["stream"])
	}
}

func TestTextPart_ImagePart(t *testing.T) {
	tp := TextPart("描述")
	if tp["type"] != "text" || tp["text"] != "描述" {
		t.Errorf("TextPart 形态错误: %v", tp)
	}
	ip := ImagePart([]byte{0x89, 0x50}, "image/png")
	img := ip["image_url"].(map[string]interface{})
	if ip["type"] != "image_url" || !strings.HasPrefix(img["url"].(string), "data:image/png;base64,") {
		t.Errorf("ImagePart bytes 应转 data-URI: %v", ip)
	}
	ipURL := ImagePart("https://x/y.png", "image/png")
	imgU := ipURL["image_url"].(map[string]interface{})
	if imgU["url"] != "https://x/y.png" {
		t.Errorf("ImagePart string 应原样: %v", ipURL)
	}
}

func TestVisionChat_AssemblesMessageAndDeclaresVision(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "  一只猫  "}}},
		})
	}))
	defer srv.Close()
	c := &LLMClient{gatewayURL: srv.URL, relayToken: "tk", httpClient: srv.Client()}

	out, err := c.VisionChat(context.Background(), "描述", [][]byte{{0x89, 0x50}}, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if out != "一只猫" {
		t.Errorf("VisionChat 应 strip 返回内容，得 %q", out)
	}
	opts := captured["request_options"].(map[string]any)
	if opts["vision_required"] != true {
		t.Errorf("VisionChat 应声明 vision_required: %v", captured)
	}
	msgs := captured["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "text" || content[1].(map[string]any)["type"] != "image_url" {
		t.Errorf("VisionChat 应组装 text+image_url content parts: %v", content)
	}
}

func TestVisionChat_NoImages_Errors(t *testing.T) {
	c := &LLMClient{gatewayURL: "http://x", relayToken: "tk", httpClient: &http.Client{}}
	if _, err := c.VisionChat(context.Background(), "x", nil, "image/png"); err == nil {
		t.Error("无图片应报错")
	}
}

func TestClassifyHTTPError_422CapabilityUnavailable(t *testing.T) {
	body := []byte(`{"code":"capability_unavailable","missing":["vision"],"message":"无 vision 模型"}`)
	err := classifyHTTPError(422, body)

	var capErr *LLMCapabilityUnavailableError
	if !errors.As(err, &capErr) {
		t.Fatalf("422 capability_unavailable 应返回 *LLMCapabilityUnavailableError，得 %T", err)
	}
	if len(capErr.Missing) != 1 || capErr.Missing[0] != "vision" {
		t.Errorf("Missing 错误: %v", capErr.Missing)
	}
	if !errors.Is(err, ErrLLMCapabilityUnavailable) {
		t.Error("应可用 ErrLLMCapabilityUnavailable 哨兵 errors.Is 判别")
	}
}

func TestClassifyHTTPError_422NonCapability_FallsThroughUpstream(t *testing.T) {
	err := classifyHTTPError(422, []byte(`{"code":"other"}`))
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("非 capability 的 422 应回落 ErrUpstream，得 %v", err)
	}
}
