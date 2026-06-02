package tester

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// 用户可见的诊断消息常量。生产代码与测试断言共享同一 symbol，避免字符串重复。
// 与 %d 拼接的消息以 Fmt 后缀单独命名，调用方使用 fmt.Sprintf 格式化。
const (
	msgInitMissingResult          = "响应缺少 result 字段"
	msgInitResultNotObject        = "result 不是对象"
	msgInitMissingProtocolVersion = "result 缺少 protocolVersion 字段"
	msgInitProtocolVersionNull    = "result.protocolVersion 为 null"
	msgListResultNotObject        = "result 不是对象"
	msgListMissingToolsField      = "result 缺少 tools 字段"
	msgListToolsNotArray          = "result.tools 不是数组"
	msgListToolDefNotObjectFmt    = "第 %d 个工具定义不是对象"
	msgListToolMissingFieldsFmt   = "第 %d 个工具定义缺少 name 或 inputSchema"
	msgCallShouldFail             = "调用不存在的工具应返回 JSON-RPC error"
)

// jsonrpcError 是 JSON-RPC 2.0 错误对象的精简表示，仅保留诊断需要的字段。
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// jsonrpcResult 是探测阶段用到的精简 JSON-RPC 响应结构，只解析 result 和 error。
type jsonrpcResult struct {
	Result any           `json:"result"`
	Error  *jsonrpcError `json:"error"`
}

// ProbeMCP 对 baseURL/mcp 端点执行 MCP Streamable HTTP 协议合规检测。
// 依次测试 initialize → tools/list → tools/call(不存在的工具) 三个方法。
// 若 initialize 失败，后续两项探测会被跳过（基础协议不通时继续探测无意义）。
func ProbeMCP(client *http.Client, baseURL string) []CheckResult {
	var results []CheckResult
	mcpURL := baseURL + "/mcp"

	// 1. initialize
	initResp, err := sendJSONRPC(client, mcpURL, 1, "initialize", map[string]any{})
	if err != nil {
		results = append(results, CheckResult{Name: "MCP initialize", Passed: false, Message: err.Error()})
		return results // 基础协议不通，后续跳过
	}
	if initResp.Result == nil {
		results = append(results, CheckResult{Name: "MCP initialize", Passed: false, Message: msgInitMissingResult})
		return results
	}
	resultMap, ok := initResp.Result.(map[string]any)
	if !ok {
		results = append(results, CheckResult{Name: "MCP initialize", Passed: false, Message: msgInitResultNotObject})
		return results
	}
	pv, present := resultMap["protocolVersion"]
	if !present {
		results = append(results, CheckResult{Name: "MCP initialize", Passed: false, Message: msgInitMissingProtocolVersion})
		return results
	}
	if pv == nil {
		results = append(results, CheckResult{Name: "MCP initialize", Passed: false, Message: msgInitProtocolVersionNull})
		return results
	}
	results = append(results, CheckResult{Name: "MCP initialize", Passed: true})

	// 2. tools/list
	listResp, err := sendJSONRPC(client, mcpURL, 2, "tools/list", map[string]any{})
	if err != nil {
		results = append(results, CheckResult{Name: "MCP tools/list", Passed: false, Message: err.Error()})
		return results
	}
	listResultMap, ok := listResp.Result.(map[string]any)
	if !ok {
		results = append(results, CheckResult{Name: "MCP tools/list", Passed: false, Message: msgListResultNotObject})
		return results
	}
	toolsRaw, present := listResultMap["tools"]
	if !present {
		results = append(results, CheckResult{Name: "MCP tools/list", Passed: false, Message: msgListMissingToolsField})
		return results
	}
	toolsList, ok := toolsRaw.([]any)
	if !ok {
		results = append(results, CheckResult{Name: "MCP tools/list", Passed: false, Message: msgListToolsNotArray})
		return results
	}
	// 验证每个 tool 是对象且包含 name + inputSchema
	for i, t := range toolsList {
		tm, ok := t.(map[string]any)
		if !ok {
			results = append(results, CheckResult{
				Name:    "MCP tools/list",
				Passed:  false,
				Message: fmt.Sprintf(msgListToolDefNotObjectFmt, i),
			})
			return results
		}
		if tm["name"] == nil || tm["inputSchema"] == nil {
			results = append(results, CheckResult{
				Name:    "MCP tools/list",
				Passed:  false,
				Message: fmt.Sprintf(msgListToolMissingFieldsFmt, i),
			})
			return results
		}
	}
	results = append(results, CheckResult{
		Name:    "MCP tools/list",
		Passed:  true,
		Message: fmt.Sprintf("发现 %d 个工具", len(toolsList)),
	})

	// 3. tools/call 不存在的工具 → 期望 JSON-RPC error
	callResp, err := sendJSONRPCRaw(client, mcpURL, 3, "tools/call", map[string]any{
		"name":      "__nonexistent__",
		"arguments": map[string]any{},
	})
	if err != nil {
		results = append(results, CheckResult{Name: "MCP tools/call 错误处理", Passed: false, Message: err.Error()})
	} else if callResp.Error != nil {
		results = append(results, CheckResult{Name: "MCP tools/call 错误处理", Passed: true})
	} else {
		results = append(results, CheckResult{
			Name:    "MCP tools/call 错误处理",
			Passed:  false,
			Message: msgCallShouldFail,
		})
	}

	return results
}

// sendJSONRPC 发送 JSON-RPC 请求并将 error 响应转换为 Go error，便于探测分支短路。
func sendJSONRPC(client *http.Client, url string, id int, method string, params any) (*jsonrpcResult, error) {
	resp, err := sendJSONRPCRaw(client, url, id, method, params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC 错误 %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp, nil
}

// sendJSONRPCRaw 发送 JSON-RPC 请求，直接返回响应（error 响应也作为正常返回）。
// 用于 tools/call 错误处理探测——此时我们**期望** error 响应存在。
func sendJSONRPCRaw(client *http.Client, url string, id int, method string, params any) (*jsonrpcResult, error) {
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	data, _ := json.Marshal(body)

	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result jsonrpcResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("响应不是有效的 JSON-RPC: %s", string(respBody))
	}
	return &result, nil
}
