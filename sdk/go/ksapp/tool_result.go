package ksapp

import (
	"encoding/json"
	"fmt"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// ToolResult 是 widgets-protocol-v1 推荐的 tool 返回值结构。
// 序列化产物匹配 MCP CallToolResult schema：
//
//	{
//	  "content": [{"type":"text","text":"..."}],
//	  "_meta": {
//	    "ui": {"widget": "..."},                  // 可选 per-call override
//	    "keystone": {"ui": {"data": {...}}}       // widget data payload
//	  }
//	}
//
// content / _meta 字段在对应入口未调用时（WithText / WithUIOverride / WithUIData）
// 都不会出现在最终 JSON 里——保持 payload 紧凑。
type ToolResult struct {
	text        string
	uiOverride  *kstypes.MetaUIDecl
	uiData      any
	uiDataReady bool
}

// NewToolResult 返回一个空 ToolResult，用链式 With* 方法填充字段。
func NewToolResult() *ToolResult {
	return &ToolResult{}
}

// WithText 写入 content[0]={"type":"text","text":...}（MCP CallToolResult 的标准
// 文本内容字段）。空字符串视为不写入。
func (r *ToolResult) WithText(text string) *ToolResult {
	r.text = text
	return r
}

// WithUIOverride 在 per-call 基础上覆盖 manifest 声明的 widget URI / sandbox_hints。
// 多数场景不需要——manifest 声明就够了；调本方法仅当 tool 在不同调用间需要
// 切换不同 widget 时才用。
func (r *ToolResult) WithUIOverride(decl kstypes.MetaUIDecl) *ToolResult {
	r.uiOverride = &decl
	return r
}

// WithUIData 注入 widget data payload。data 应是 widget schema 类型实例，
// 例如 kstypes.WidgetDiffReviewV1{...}。
//
// MarshalJSON 时调用 data.Validate()（如果实现了 interface{ Validate() error }）；
// 失败时 MarshalJSON 返回 error，把 widget schema 校验错误透传给 caller。
//
// 接 nil 也会触发 _meta.keystone.ui.data 序列化（产生 null）；这是 caller 的
// 责任：要么传非 nil 数据，要么不调本方法。
func (r *ToolResult) WithUIData(data any) *ToolResult {
	r.uiData = data
	r.uiDataReady = true
	return r
}

// MarshalJSON 序列化 ToolResult 为 MCP CallToolResult JSON。
// 实现 json.Marshaler 接口。
func (r *ToolResult) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if r.text != "" {
		out["content"] = []map[string]any{{"type": "text", "text": r.text}}
	}
	meta := map[string]any{}
	if r.uiOverride != nil {
		meta["ui"] = r.uiOverride
	}
	if r.uiDataReady {
		if v, ok := r.uiData.(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return nil, fmt.Errorf("widget data validation failed: %w", err)
			}
		}
		raw, err := json.Marshal(r.uiData)
		if err != nil {
			return nil, fmt.Errorf("widget data marshal failed: %w", err)
		}
		meta["keystone"] = map[string]any{
			"ui": map[string]any{"data": json.RawMessage(raw)},
		}
	}
	if len(meta) > 0 {
		out["_meta"] = meta
	}
	return json.Marshal(out)
}
