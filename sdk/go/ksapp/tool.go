package ksapp

import (
	"context"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// ToolHandler 是工具调用的处理函数签名。
// ctx 中可能携带 Keystone 运行时注入的上下文信息，
// 通过 ResourceScope / ExecutionID / TaskID 等辅助函数读取。
type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

// ToolDef 描述一个已注册工具的元信息及其处理器。
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON Schema，可选；为 nil 时使用空 object schema
	Handler     ToolHandler
	// UIBinding widgets-protocol-v1 widget 绑定（v0.6.0 新增，可选）。
	// 通过 ToolBuilder.WithToolUI 设置；非 nil 时 /meta 会写入
	// tools[]._meta.ui 并声明 capabilities.ui.enabled。
	UIBinding *kstypes.ToolUIBinding
	// Annotations 是 MCP 2025-03-26 tool annotations。通过 ToolBuilder.WithAnnotations
	// 设置；nil 表示不声明。Mux() 转发到 mcpproto.ToolDef.Annotations。
	Annotations map[string]any
}
