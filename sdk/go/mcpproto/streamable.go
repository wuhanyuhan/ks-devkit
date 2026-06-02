package mcpproto

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// mcpProtocolVersion 是当前 SDK 支持的 MCP Streamable HTTP 协议版本。
// Spec: https://spec.modelcontextprotocol.io/specification/2025-03-26/
const mcpProtocolVersion = "2025-03-26"

// NewStreamableHTTPHandler 创建 MCP Streamable HTTP 端点（POST /mcp）的 http.Handler。
// 使用 JSON-RPC 2.0 信封，支持 initialize、tools/list、tools/call 方法。
// callTool 回调由上层提供，负责执行具体工具逻辑。
func NewStreamableHTTPHandler(appID, appVersion string, tools []ToolDef, callTool CallToolFunc) http.Handler {
	toolMap := make(map[string]ToolDef, len(tools))
	for _, t := range tools {
		toolMap[t.Name] = t
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
			return
		}

		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// parse error 按 JSON-RPC 2.0 §4.2 仍需返回响应（id 为 null）。
			writeJSONRPCError(w, nil, errCodeParseError, "JSON 解析失败")
			return
		}

		// JSON-RPC 2.0 §4.1：请求无 id 字段时为 notification，服务端不应有响应体。
		// 真实 MCP 客户端会发送 notifications/initialized 等通知，必须正确处理避免握手失败。
		// 注意：解码成功后 req.ID == nil 表示客户端没有传 id 字段；与上面的 parse error 路径区分开。
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		if req.JSONRPC != "2.0" {
			writeJSONRPCError(w, req.ID, errCodeInvalidRequest, "仅支持 JSON-RPC 2.0")
			return
		}

		switch req.Method {
		case "initialize":
			handleInitialize(w, req, appID, appVersion)
		case "tools/list":
			handleToolsList(w, req, tools)
		case "tools/call":
			handleToolsCall(w, r, req, toolMap, callTool)
		default:
			writeJSONRPCError(w, req.ID, errCodeMethodNotFound, "未知方法: "+req.Method)
		}
	})
}

func handleInitialize(w http.ResponseWriter, req JSONRPCRequest, appID, appVersion string) {
	writeJSONRPCResult(w, req.ID, map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    appID,
			"version": appVersion,
		},
	})
}

func handleToolsList(w http.ResponseWriter, req JSONRPCRequest, tools []ToolDef) {
	toolDefs := make([]map[string]any, len(tools))
	for i, t := range tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		def := map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": schema,
		}
		if len(t.Annotations) > 0 {
			def["annotations"] = t.Annotations
		}
		toolDefs[i] = def
	}
	writeJSONRPCResult(w, req.ID, map[string]any{"tools": toolDefs})
}

func handleToolsCall(w http.ResponseWriter, r *http.Request, req JSONRPCRequest, toolMap map[string]ToolDef, callTool CallToolFunc) {
	// 缺少 params 字段单独报错，避免 json.Unmarshal 对 nil 返回 "unexpected end of JSON input" 这种迷惑性消息。
	if req.Params == nil {
		writeJSONRPCError(w, req.ID, errCodeInvalidParams, "tools/call 缺少 params 字段")
		return
	}

	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
		Meta      map[string]any `json:"_meta"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		// JSON 结构错误本身不是敏感信息，原始 err 可暴露给客户端方便排查。
		writeJSONRPCError(w, req.ID, errCodeInvalidParams, "tools/call 参数解析失败: "+err.Error())
		return
	}

	if params.Name == "" {
		writeJSONRPCError(w, req.ID, errCodeInvalidParams, "tools/call 缺少 name 参数")
		return
	}

	if _, ok := toolMap[params.Name]; !ok {
		writeJSONRPCError(w, req.ID, errCodeInvalidParams, "工具不存在: "+params.Name)
		return
	}

	ctx := WithMeta(r.Context(), params.Meta)
	result, err := callTool(ctx, params.Name, params.Arguments)
	if err != nil {
		// handler 返回的 error 可能含内部路径、SQL 片段等敏感信息，不能直接回给客户端。
		// 完整错误通过 slog 记录到服务端日志，客户端只收到固定提示。
		slog.Error("tool handler 执行失败",
			"tool", params.Name,
			"request_id", req.ID,
			"error", err)
		writeJSONRPCError(w, req.ID, errCodeInternal, "工具执行失败")
		return
	}

	// 将结果转换为 MCP content 格式
	content := resultToContent(result)
	writeJSONRPCResult(w, req.ID, map[string]any{"content": content})
}

// resultToContent 将 handler 返回值转换为 MCP content 数组。
// string → text content；其它类型 → JSON 序列化后的 text content。
// nil 会被序列化为字符串 "null"，与 json.Marshal(nil) 行为一致。
func resultToContent(result any) []map[string]any {
	switch v := result.(type) {
	case string:
		return []map[string]any{{"type": "text", "text": v}}
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return []map[string]any{{"type": "text", "text": "序列化结果失败"}}
		}
		return []map[string]any{{"type": "text", "text": string(data)}}
	}
}

func writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCError{
		JSONRPC: "2.0", ID: id,
		Error: &JSONRPCErrBody{Code: code, Message: message},
	})
}
