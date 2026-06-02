package ksapp

import kstypes "github.com/wuhanyuhan/ks-types"

// ToolBuilder 通过链式 API 构造 ToolDef，是 widgets-protocol-v1 推荐的
// tool 注册方式。既有 App.Tool / App.ToolWithSchema 仍保留可用。
//
// 使用：
//
//	app.RegisterTool(ksapp.NewTool("review_draft").
//	    WithDescription("审阅指定 draft").
//	    WithInputSchema(reviewDraftSchema).
//	    WithToolUI(kstypes.ToolUIBinding{Widget: "ks://widgets/diff-review@v1"}).
//	    WithHandler(reviewDraftHandler))
type ToolBuilder struct {
	def ToolDef
}

// NewTool 用 name 起一个新 ToolBuilder。后续链式调用填充字段，最后由
// App.RegisterTool 消费 Build() 结果。
func NewTool(name string) *ToolBuilder {
	return &ToolBuilder{def: ToolDef{Name: name}}
}

// WithDescription 设置工具描述（将出现在 /meta.tools[].description 与 MCP tools/list）。
func (b *ToolBuilder) WithDescription(desc string) *ToolBuilder {
	b.def.Description = desc
	return b
}

// WithInputSchema 设置参数 JSON Schema（将出现在 MCP tools/list 的 inputSchema 字段）。
// 不调用时 mcpproto 默认用空 object schema。
func (b *ToolBuilder) WithInputSchema(schema map[string]any) *ToolBuilder {
	b.def.InputSchema = schema
	return b
}

// WithToolUI 声明 widget binding（widgets-protocol-v1）。声明后会在 /meta 中
// 注入 tools[]._meta.ui，并自动声明 capabilities.ui.enabled = true。
//
// 不调用时 binding 为 nil，行为退化为传统无 UI 的 MCP tool。
func (b *ToolBuilder) WithToolUI(binding kstypes.ToolUIBinding) *ToolBuilder {
	b.def.UIBinding = &binding
	return b
}

// WithAnnotations 设置 MCP 2025-03-26 tool annotations。
//
// 推荐字段（按 MCP spec）：
//   - readOnlyHint   (bool)：仅读不写
//   - destructiveHint(bool)：可能产生不可逆副作用
//   - idempotentHint (bool)：相同入参重复调用结果一致
//   - openWorldHint  (bool)：访问外部系统（与 MCP server 之外的世界交互）
//   - title          (string)：工具显示名（可选）
//
// 示例：
//
//	builder.WithAnnotations(map[string]any{
//	    "readOnlyHint":    true,
//	    "destructiveHint": false,
//	    "idempotentHint":  true,
//	    "openWorldHint":   true,
//	})
func (b *ToolBuilder) WithAnnotations(ann map[string]any) *ToolBuilder {
	b.def.Annotations = ann
	return b
}

// WithHandler 设置 tool 调用处理函数，handler 通过 App.RegisterTool 注册后
// 由 mcp tools/call 路由触发。
func (b *ToolBuilder) WithHandler(h ToolHandler) *ToolBuilder {
	b.def.Handler = h
	return b
}

// Build 输出最终 ToolDef。一般由 App.RegisterTool 内部调用，外部不需要直接调。
func (b *ToolBuilder) Build() ToolDef {
	return b.def
}
