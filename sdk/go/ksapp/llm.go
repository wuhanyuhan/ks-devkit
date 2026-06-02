package ksapp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultLLMTimeout 是 HTTP 客户端的默认超时。LLM 调用可能长尾，120s 作为兜底。
const defaultLLMTimeout = 120 * time.Second

// ── 错误常量 ────────────────────────────────────────────────────────
//
// 注意：ErrNotConfigured 已在 errors.go 中定义（统一哨兵，
// "配置未提供 / 凭证缺失"语义），本文件不重复声明。调用方应使用
// NewErrNotConfigured("llm-relay", ...) 构造带 scope 与 detail 的实例，
// 通过 errors.Is 即可断言。

// ErrUnauthorized 401/403。网关判定 relay token 无效。
var ErrUnauthorized = errors.New("ERR_UNAUTHORIZED: LLM relay 鉴权失败")

// ErrRateLimited 429。网关限流。
var ErrRateLimited = errors.New("ERR_RATE_LIMITED: LLM relay 被限流")

// ErrUpstream 5xx 或上游其他错误。
var ErrUpstream = errors.New("ERR_UPSTREAM: LLM relay 上游错误")

// ErrLLMCapabilityUnavailable 422 capability_unavailable：现场无满足所需能力（如 vision）的模型。
// 与 capability-mesh 的 ErrCapabilityUnavailable（errors_capability.go，dispatch 域）区分。
var ErrLLMCapabilityUnavailable = errors.New("ERR_CAPABILITY_UNAVAILABLE: LLM relay 无满足能力的模型")

// LLMCapabilityUnavailableError 携带缺失能力列表，供调用方判别后自行降级。
type LLMCapabilityUnavailableError struct {
	Missing []string
	Status  int
}

func (e *LLMCapabilityUnavailableError) Error() string {
	return fmt.Sprintf("ERR_CAPABILITY_UNAVAILABLE: missing=%v status=%d", e.Missing, e.Status)
}

func (e *LLMCapabilityUnavailableError) Unwrap() error { return ErrLLMCapabilityUnavailable }

// parseCapabilityMissing 解析 relay 422 body {"code":"capability_unavailable","missing":[...]}。
// 非该形态返回 (nil, false)。
func parseCapabilityMissing(body []byte) ([]string, bool) {
	var p struct {
		Code    string   `json:"code"`
		Missing []string `json:"missing"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.Code != "capability_unavailable" {
		return nil, false
	}
	return p.Missing, true
}

// ── 核心类型（JSON tag 与 keystone/pkg/llmclient/types.go 对齐） ────

// ChatRequest 一次聊天请求的所有参数。
type ChatRequest struct {
	Model          string                   `json:"model,omitempty"`
	Messages       []Message                `json:"messages"`
	Tools          []map[string]interface{} `json:"tools,omitempty"`
	Temperature    *float64                 `json:"temperature,omitempty"`
	MaxTokens      int                      `json:"max_tokens,omitempty"`
	Stream         bool                     `json:"stream,omitempty"`
	RequestOptions map[string]interface{}   `json:"request_options,omitempty"`

	// intent 一等字段：序列化时降解进 request_options（见 MarshalJSON），不直接上 wire。
	// 值字符串与 ks-types LLMTier/LLMCapability/ReasoningMode 一致（economy/standard/flagship；vision；on/off/auto）。
	Tier                string   `json:"-"`
	Reasoning           string   `json:"-"`
	RequireCapabilities []string `json:"-"`
}

// MarshalJSON 把 intent 一等字段折叠进 request_options 约定 key，对齐 python to_dict：
//   - RequireCapabilities 每项 → "<cap>_required": true
//   - Tier → "tier"；Reasoning → "reasoning_mode"
//
// relay 顶层 schema 不认识这些字段，故只走 request_options。覆盖 Chat 与 StreamChat 两条路径。
func (r ChatRequest) MarshalJSON() ([]byte, error) {
	type alias ChatRequest // 去掉 MarshalJSON 方法，避免无限递归
	a := alias(r)
	opts := make(map[string]interface{}, len(a.RequestOptions)+len(a.RequireCapabilities)+2)
	for k, v := range a.RequestOptions {
		opts[k] = v
	}
	for _, c := range a.RequireCapabilities {
		opts[c+"_required"] = true
	}
	if a.Tier != "" {
		opts["tier"] = a.Tier
	}
	if a.Reasoning != "" {
		opts["reasoning_mode"] = a.Reasoning
	}
	if len(opts) > 0 {
		a.RequestOptions = opts
	} else {
		a.RequestOptions = nil
	}
	return json.Marshal(a)
}

// Message 一条对话消息。Content 可以是 string 或结构化内容（多模态，当前只支持 string）。
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
}

// ToolCall 非流式响应中一次完整工具调用。
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 工具调用的函数名与参数（JSON 字符串）。
// 两个字段都带 omitempty：
//   - 非流式 ToolCall 场景：OpenAI 规约保证 Name 必填，加 omitempty 无副作用
//   - 流式 ToolCallDelta 场景：每个 chunk 可能只含 Name 或只含 Arguments 增量
//     部分，omitempty 保证序列化与 SSE 语义一致
type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolCallDelta 流式工具调用的增量片段。
type ToolCallDelta struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

// ChatResponse 非流式聊天请求的完整响应。
type ChatResponse struct {
	Content      string     `json:"content"`
	FinishReason string     `json:"finish_reason"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        Usage      `json:"usage"`
}

// Chunk 流式响应中的一个增量块。
type Chunk struct {
	DeltaContent   string          `json:"delta_content,omitempty"`
	FinishReason   string          `json:"finish_reason,omitempty"`
	ToolCallsDelta []ToolCallDelta `json:"tool_calls_delta,omitempty"`
	Usage          *Usage          `json:"usage,omitempty"`
}

// Usage 本次请求的 Token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ── 客户端 ──────────────────────────────────────────────────────────

// LLMClient Keystone LLM Relay 客户端。
// 通过 Keystone 网关调用大模型，无需自行管理 API key。
// 要求 manifest 声明 permissions.llm: host_proxy，Keystone 安装应用时注入 KS_RELAY_TOKEN。
type LLMClient struct {
	gatewayURL string
	relayToken string
	httpClient *http.Client
}

// newLLMClient 从环境变量读取配置，创建客户端。
// KS_GATEWAY_URL 默认 http://localhost:9988。
// KS_RELAY_TOKEN 由 Keystone 装机时注入；本地开发需手动设置。
func newLLMClient() *LLMClient {
	gatewayURL := os.Getenv("KS_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:9988"
	}
	gatewayURL = strings.TrimRight(gatewayURL, "/") // 去掉末尾 /，避免拼接产生 http://host//v1/... 双斜杠 URL
	return &LLMClient{
		gatewayURL: gatewayURL,
		relayToken: firstEnv("KS_RELAY_TOKEN", "KEYSTONE_RELAY_TOKEN"),
		httpClient: &http.Client{Timeout: defaultLLMTimeout},
	}
}

// Chat 发送非流式聊天请求到 keystone relay 端点。
// 路径: POST /v1/mcp/relay/chat/completions
// 返回 ChatResponse 或按 HTTP 状态码分类的错误。
func (c *LLMClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if c.relayToken == "" {
		return nil, NewErrNotConfigured("llm-relay", "KS_RELAY_TOKEN 未设置，请确认 manifest 声明 permissions.llm: host_proxy")
	}

	// 确保 stream=false
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求: %w", err)
	}

	url := c.gatewayURL + "/v1/mcp/relay/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.relayToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求 LLM 网关: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, classifyHTTPError(resp.StatusCode, respBody)
	}

	// 解析 OpenAI 兼容响应
	var raw struct {
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("解析响应: %w", err)
	}

	out := &ChatResponse{Usage: raw.Usage}
	if len(raw.Choices) > 0 {
		ch := raw.Choices[0]
		out.Content = ch.Message.Content
		out.FinishReason = ch.FinishReason
		out.ToolCalls = ch.Message.ToolCalls
	}
	return out, nil
}

// StreamChat 发送流式聊天请求，每个增量通过 emit 回调传递。
// 消费方中途放弃需取消 ctx，不能通过回调反向终止。
func (c *LLMClient) StreamChat(ctx context.Context, req ChatRequest, emit func(Chunk)) error {
	if c.relayToken == "" {
		return NewErrNotConfigured("llm-relay", "KS_RELAY_TOKEN 未设置，请确认 manifest 声明 permissions.llm: host_proxy")
	}

	// 强制 stream=true
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求: %w", err)
	}

	url := c.gatewayURL + "/v1/mcp/relay/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.relayToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求 LLM 网关: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return classifyHTTPError(resp.StatusCode, errBody)
	}

	return parseSSEStream(ctx, resp.Body, emit)
}

// parseSSEStream 解析 OpenAI 格式 SSE 流。
// 行格式: `data: <json>\n\n`，结尾 `data: [DONE]\n\n`。
func parseSSEStream(ctx context.Context, body io.Reader, emit func(Chunk)) error {
	scanner := bufio.NewScanner(body)
	// 单个 SSE event 可能很大（tool_calls arguments 累积）
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return nil
		}

		var raw struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role      string                   `json:"role"`
					Content   string                   `json:"content"`
					ToolCalls []map[string]interface{} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &raw); err != nil {
			// 忽略单行解析失败，继续后续 chunk（容错）
			continue
		}

		chunk := Chunk{Usage: raw.Usage}
		if len(raw.Choices) > 0 {
			ch := raw.Choices[0]
			chunk.DeltaContent = ch.Delta.Content
			chunk.FinishReason = ch.FinishReason
			chunk.ToolCallsDelta = convertToolCallsDelta(ch.Delta.ToolCalls)
		}

		// 空 chunk（例如只有 role 的 first chunk 且无 content）不 emit
		if chunk.DeltaContent == "" &&
			chunk.FinishReason == "" &&
			len(chunk.ToolCallsDelta) == 0 &&
			chunk.Usage == nil {
			continue
		}

		emit(chunk)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE scanner: %w", err)
	}
	return nil
}

// convertToolCallsDelta 将 OpenAI delta.tool_calls 转为 SDK 的 ToolCallDelta 列表。
func convertToolCallsDelta(raw []map[string]interface{}) []ToolCallDelta {
	if len(raw) == 0 {
		return nil
	}
	result := make([]ToolCallDelta, 0, len(raw))
	for _, tc := range raw {
		d := ToolCallDelta{}
		if idx, ok := tc["index"].(float64); ok {
			d.Index = int(idx)
		}
		if id, ok := tc["id"].(string); ok {
			d.ID = id
		}
		if typ, ok := tc["type"].(string); ok {
			d.Type = typ
		}
		if fn, ok := tc["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				d.Function.Name = name
			}
			if args, ok := fn["arguments"].(string); ok {
				d.Function.Arguments = args
			}
		}
		result = append(result, d)
	}
	return result
}

// classifyHTTPError 将 HTTP 错误状态码转成具名错误。
func classifyHTTPError(statusCode int, body []byte) error {
	bodyStr := string(body)
	if len(bodyStr) > 500 {
		bodyStr = bodyStr[:500]
	}
	switch {
	case statusCode == http.StatusUnauthorized, statusCode == http.StatusForbidden:
		return fmt.Errorf("%w: status=%d body=%s", ErrUnauthorized, statusCode, bodyStr)
	case statusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: status=%d body=%s", ErrRateLimited, statusCode, bodyStr)
	case statusCode == http.StatusUnprocessableEntity:
		if missing, ok := parseCapabilityMissing(body); ok {
			return &LLMCapabilityUnavailableError{Missing: missing, Status: statusCode}
		}
		return fmt.Errorf("%w: status=%d body=%s", ErrUpstream, statusCode, bodyStr)
	default:
		return fmt.Errorf("%w: status=%d body=%s", ErrUpstream, statusCode, bodyStr)
	}
}

// TextPart 构造 OpenAI 文本内容块（对齐 python text_part）。
func TextPart(text string) map[string]interface{} {
	return map[string]interface{}{"type": "text", "text": text}
}

// ImagePart 构造 OpenAI 图片内容块（对齐 python image_part）。
// source 为 []byte → base64 data-URI（用 mime）；为 string（http(s):// 或 data:）→ 原样作 url。
func ImagePart(source interface{}, mime string) map[string]interface{} {
	var url string
	switch s := source.(type) {
	case []byte:
		url = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(s)
	case string:
		url = s
	}
	return map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}
}

// VisionChat 带一张或多张图片问模型，返回文本。自动声明 vision 能力路由
// （RequireCapabilities=["vision"] → request_options.vision_required，由 relay 严格过滤）。
// images 为空返回错误；失败按 classifyHTTPError 类型化错误返回（含 LLMCapabilityUnavailableError）。
func (c *LLMClient) VisionChat(ctx context.Context, prompt string, images [][]byte, mime string) (string, error) {
	if len(images) == 0 {
		return "", fmt.Errorf("VisionChat 需要至少一张图片")
	}
	content := make([]interface{}, 0, len(images)+1)
	content = append(content, TextPart(prompt))
	for _, img := range images {
		content = append(content, ImagePart(img, mime))
	}
	temp := 0.2
	resp, err := c.Chat(ctx, ChatRequest{
		Messages:            []Message{{Role: "user", Content: content}},
		Temperature:         &temp,
		RequireCapabilities: []string{"vision"},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// firstEnv 返回第一个非空的环境变量值。用于兼容多个 env 名（如 relay token 的两种命名）。
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
