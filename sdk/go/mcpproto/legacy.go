package mcpproto

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// NewLegacyListHandler 创建旧版 GET /mcp/tools/list 端点的 http.Handler。
//
// Deprecated: 此端点是早期的自定义 JSON 协议，已被 NewStreamableHTTPHandler
// 实现的标准 MCP Streamable HTTP 端点替代。保留仅用于过渡兼容，
// 将在 Keystone 客户端全部迁移完成后移除。
func NewLegacyListHandler(tools []ToolDef) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 与 streamable handleToolsList 保持一致：annotations 透传到 list 响应。
		// 不强行加 inputSchema 字段以避免 legacy 端点协议形态变化（既有客户端
		// 只期望 name/description）。
		toolList := make([]map[string]any, len(tools))
		for i, t := range tools {
			item := map[string]any{
				"name":        t.Name,
				"description": t.Description,
			}
			if len(t.Annotations) > 0 {
				item["annotations"] = t.Annotations
			}
			toolList[i] = item
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tools": toolList})
	})
}

// NewLegacyCallHandler 创建旧版 POST /mcp/tools/call 端点的 http.Handler。
// callTool 回调由上层提供，负责执行具体工具逻辑。
//
// Deprecated: 同 NewLegacyListHandler。
func NewLegacyCallHandler(tools []ToolDef, callTool CallToolFunc) http.Handler {
	toolNames := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		toolNames[t.Name] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "请求体解析失败: " + err.Error()})
			return
		}

		if _, ok := toolNames[req.Name]; !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "tool not found"})
			return
		}

		meta, _ := req.Params["_meta"].(map[string]any)
		delete(req.Params, "_meta")
		ctx := WithMeta(r.Context(), meta)
		result, err := callTool(ctx, req.Name, req.Params)
		if err != nil {
			// handler 返回的 error 可能含内部路径、SQL 片段等敏感信息，不能直接回给客户端。
			// 与新端点 handleToolsCall 保持一致：完整错误通过 slog 记录到服务端日志，
			// 客户端只收到固定提示。
			slog.Error("tool handler 执行失败（legacy endpoint）",
				"tool", req.Name,
				"error", err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "工具执行失败"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
	})
}
