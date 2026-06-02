package mcpproto

import "context"

// ToolDef 描述一个工具的 MCP 协议元数据（不含 handler）。
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	// Annotations 是 MCP 2025-03-26 规范的 tool annotations（readOnlyHint /
	// destructiveHint / idempotentHint / openWorldHint）。nil 表示不声明。
	Annotations map[string]any
}

// CallToolFunc 是执行工具的回调函数。
// mcpproto 调用它来执行具体工具。如果返回 error，mcpproto 负责脱敏：
// 完整堆栈记到服务端日志，客户端只收到固定的 "工具执行失败" 提示。
type CallToolFunc func(ctx context.Context, name string, args map[string]any) (any, error)
