package ksapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DispatcherClient 是与 keystone capability dispatcher 通讯的 HTTP 客户端。
//
// 设计选择：本类型放在 ksapp 主包（不放 ksapp/keystoneclient 子包），原因是
// 内部 mapHTTPError 必须直接构造 ksapp 错误层级（NewCapabilityNotFound /
// NewTokenAudienceMismatch / RateLimitErr 等），若放子包反向 import 主包会循环。
type DispatcherClient struct {
	gatewayURL string
	appToken   string
	httpClient *http.Client
}

// NewDispatcherClient 用 gateway URL + app token 构造一个 client。
func NewDispatcherClient(gatewayURL, appToken string) *DispatcherClient {
	return &DispatcherClient{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		appToken:   appToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// InvokeOptions 是 Invoke 入参。
type InvokeOptions struct {
	Capability        string
	Args              map[string]any
	Mode              string // "sync" / "async" / "auto"
	IdempotencyKey    string
	TimeoutMsOverride *int
	ChainHeader       string
	ChainID           string
	// OnBehalfOfUserID 调用链发起人 user_id（穿透多跳 capability mesh）。
	// app-to-app 调用时，caller 应从 CapabilityContext.UserID() 取并 atoi 后传入。
	// 0 表示「无 user 上下文」（不会被透传到 keystone payload，由 dispatcher 默认行为兜底）。
	OnBehalfOfUserID int64
}

// InvokeResult sync / async 二选一。
type InvokeResult struct {
	Sync  *InvokeSyncResult
	Async *InvokeAsyncResult
}

type InvokeSyncResult struct {
	Result     map[string]any `json:"result"`
	DurationMs int64          `json:"duration_ms"`
}

type InvokeAsyncResult struct {
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	SubmittedAt string `json:"submitted_at"`
	TimeoutAt   string `json:"timeout_at"`
}

// TaskSnapshot 是 GET /v1/user-tasks/:id 的返回快照。
type TaskSnapshot struct {
	TaskID        string         `json:"task_id"`
	Status        string         `json:"status"`
	CanonicalName string         `json:"canonical_name"`
	Percent       int            `json:"percent"`
	StageMessage  string         `json:"stage_message"`
	Result        map[string]any `json:"result"`
	ErrorCode     string         `json:"error_code"`
	ErrorMessage  string         `json:"error_message"`
}

// Invoke POST /v1/apps/self/invoke。sync 时返 InvokeResult.Sync；async 返 .Async。
func (c *DispatcherClient) Invoke(ctx context.Context, opts InvokeOptions) (*InvokeResult, error) {
	payload := map[string]any{
		"capability": opts.Capability,
		"mode":       opts.Mode,
	}
	if opts.Args != nil {
		payload["args"] = opts.Args
	}
	if opts.IdempotencyKey != "" {
		payload["idempotency_key"] = opts.IdempotencyKey
	}
	if opts.TimeoutMsOverride != nil {
		payload["timeout_ms_override"] = *opts.TimeoutMsOverride
	}
	if opts.OnBehalfOfUserID > 0 {
		payload["on_behalf_of_user_id"] = opts.OnBehalfOfUserID
	}
	data, err := c.postWithHeaders(ctx, "/v1/apps/self/invoke", payload, opts.Capability, map[string]string{
		headerCallChain: opts.ChainHeader,
		headerChainID:   opts.ChainID,
	})
	if err != nil {
		return nil, err
	}
	if tid, ok := data["task_id"].(string); ok && tid != "" {
		out := &InvokeAsyncResult{TaskID: tid}
		out.Status, _ = data["status"].(string)
		out.SubmittedAt, _ = data["submitted_at"].(string)
		out.TimeoutAt, _ = data["timeout_at"].(string)
		return &InvokeResult{Async: out}, nil
	}
	sync := &InvokeSyncResult{}
	if r, ok := data["result"].(map[string]any); ok {
		sync.Result = r
	}
	if d, ok := data["duration_ms"].(float64); ok {
		sync.DurationMs = int64(d)
	}
	return &InvokeResult{Sync: sync}, nil
}

// ReportProgress POST /v1/user-tasks/:task_id/progress。
// 失败时 best-effort：返 nil（业务 handler 不应因 progress 失败而失败）。
func (c *DispatcherClient) ReportProgress(ctx context.Context, taskID, stage string, percent *int) error {
	payload := map[string]any{"stage_message": stage}
	if percent != nil {
		payload["percent"] = *percent
	}
	_, err := c.post(ctx, "/v1/user-tasks/"+taskID+"/progress", payload, "")
	return err
}

// GetTask GET /v1/user-tasks/:task_id。
// 404 在 mapHTTPError 内默认返 CapabilityNotFound；此处 caller 端 catch 后重抛 TaskNotFound
// （与 Python 端实施偏离对齐：404 不在 mapHTTPError 内嗅探 task vs capability）。
func (c *DispatcherClient) GetTask(ctx context.Context, taskID string) (*TaskSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.gatewayURL+"/v1/user-tasks/"+taskID, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build req: %v", ErrCapabilityUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.appToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: network: %v", ErrCapabilityUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, remapNotFoundToTask(mapHTTPError(resp.StatusCode, body, resp.Header, taskID), taskID)
	}
	var env struct {
		Code int          `json:"code"`
		Data TaskSnapshot `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("%w: parse json: %v", ErrBackendError, err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("%w: business error code=%d", ErrBackendError, env.Code)
	}
	return &env.Data, nil
}

// CancelTask POST /v1/user-tasks/:task_id/cancel。
// 同 GetTask：404 caller 端重映射为 TaskNotFound。
func (c *DispatcherClient) CancelTask(ctx context.Context, taskID string) error {
	_, err := c.post(ctx, "/v1/user-tasks/"+taskID+"/cancel", map[string]any{}, taskID)
	return remapNotFoundToTask(err, taskID)
}

// post 是 Invoke / ReportProgress / CancelTask 共用的 JSON POST helper。
// capHint 仅用于 404 mapHTTPError 时构造 CapabilityNotFound 的 canonical_name 字段。
func (c *DispatcherClient) post(ctx context.Context, path string, payload map[string]any, capHint string) (map[string]any, error) {
	return c.postWithHeaders(ctx, path, payload, capHint, nil)
}

func (c *DispatcherClient) postWithHeaders(ctx context.Context, path string, payload map[string]any, capHint string, headers map[string]string) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gatewayURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build req: %v", ErrCapabilityUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.appToken)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: network: %v", ErrCapabilityUnavailable, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, mapHTTPError(resp.StatusCode, respBody, resp.Header, capHint)
	}
	var env struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("%w: parse json: %v", ErrBackendError, err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("%w: business code=%d message=%s", ErrBackendError, env.Code, env.Message)
	}
	return env.Data, nil
}

// mapHTTPError 把 keystone 标准错误响应映射成 ksapp 错误层级。
// 404 默认返 CapabilityNotFound；task 相关 caller（GetTask/CancelTask）自行重映射。
func mapHTTPError(status int, body []byte, headers http.Header, hint string) error {
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &env)
	retryAfter := retryAfterMs(headers)
	switch status {
	case 400:
		return fmt.Errorf("%w: %s", ErrInvalidArgs, env.Message)
	case 401:
		switch env.Code {
		case 40103:
			return NewTokenAudienceMismatch(env.Message)
		case 40102:
			return fmt.Errorf("%w: %s", ErrTokenExpired, env.Message)
		}
		return fmt.Errorf("%w: %s", ErrTokenInvalid, env.Message)
	case 403:
		if env.Code == 40301 {
			return fmt.Errorf("%w: %s", ErrCapabilityDisabled, env.Message)
		}
		return fmt.Errorf("%w: %s", ErrCapabilityForbidden, env.Message)
	case 404:
		return NewCapabilityNotFound(hint)
	case 408:
		return fmt.Errorf("%w: %s", ErrTimeout, env.Message)
	case 429:
		return &RateLimitErr{Message: env.Message, RetryAfterMs: retryAfter, Sentinel: ErrRateLimitError}
	case 451:
		return fmt.Errorf("%w: %s", ErrGuardrailBlocked, env.Message)
	case 502:
		return fmt.Errorf("%w: %s", ErrBackendError, env.Message)
	case 503:
		return NewCapabilityUnavailable(env.Message, retryAfter)
	case 508:
		return fmt.Errorf("%w: %s", ErrLoopDetected, env.Message)
	}
	return fmt.Errorf("%w: unexpected http status=%d body=%s", ErrBackendError, status, string(body))
}

// remapNotFoundToTask 把 CapabilityNotFound 错误重映射为 TaskNotFound（GetTask/CancelTask 用）。
// 404 在 mapHTTPError 里默认按 capability 处理；task caller 自己重抛是为了避免在 mapper
// 里嗅探 message 字段（Python 端实施期同样的偏离）。
func remapNotFoundToTask(err error, taskID string) error {
	if err == nil {
		return nil
	}
	var cnf *CapabilityNotFoundErr
	if errors.As(err, &cnf) {
		return NewTaskNotFound(taskID)
	}
	return err
}

func retryAfterMs(h http.Header) int {
	ra := h.Get("Retry-After")
	if ra == "" {
		return 0
	}
	if s, err := strconv.ParseFloat(ra, 64); err == nil {
		return int(s * 1000)
	}
	return 0
}
