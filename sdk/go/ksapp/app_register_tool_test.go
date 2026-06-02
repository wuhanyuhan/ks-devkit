package ksapp

import (
	"context"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// 复用 app_declare_test.go 已定义的 getMeta / getMetaRaw helper（同 package）。

// TestApp_RegisterTool_BindingAppearsInMeta 验证 ToolBuilder 注册的工具
// 在 /meta 中正确反射 _meta.ui binding 与 capabilities.ui.enabled。
func TestApp_RegisterTool_BindingAppearsInMeta(t *testing.T) {
	t.Parallel()
	app := New("test-app").
		RegisterTool(NewTool("review_draft").
			WithDescription("审稿").
			WithToolUI(kstypes.ToolUIBinding{Widget: "ks://widgets/diff-review@v1"}).
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil }))

	resp := getMeta(t, app.Mux())

	if resp.Capabilities == nil || resp.Capabilities.UI == nil {
		t.Fatalf("Capabilities.UI nil, got: %+v", resp.Capabilities)
	}
	if !resp.Capabilities.UI.Enabled {
		t.Errorf("Capabilities.UI.Enabled should be true")
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Tools))
	}
	if resp.Tools[0].Meta == nil || resp.Tools[0].Meta.UI == nil {
		t.Fatalf("tool meta.UI nil, got: %+v", resp.Tools[0].Meta)
	}
	if resp.Tools[0].Meta.UI.Widget != "ks://widgets/diff-review@v1" {
		t.Errorf("widget URI: got %q", resp.Tools[0].Meta.UI.Widget)
	}
}

// TestApp_RegisterTool_NoBindingNoCapabilities 验证：不带 UIBinding 注册的工具
// 不会产生 capabilities.ui 字段。
func TestApp_RegisterTool_NoBindingNoCapabilities(t *testing.T) {
	t.Parallel()
	app := New("test-app").
		RegisterTool(NewTool("plain").
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil }))
	resp := getMeta(t, app.Mux())
	if resp.Capabilities != nil {
		t.Errorf("Capabilities should be nil when no tool has UIBinding, got: %+v", resp.Capabilities)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Tools))
	}
	if resp.Tools[0].Meta != nil {
		t.Errorf("Meta should be nil when tool has no UIBinding, got: %+v", resp.Tools[0].Meta)
	}
}

// TestApp_RegisterTool_RawJSON_OmitsCapabilitiesWhenEmpty 验证原始 JSON 中
// capabilities 字段在无 binding 时省略（omitempty）。
func TestApp_RegisterTool_RawJSON_OmitsCapabilitiesWhenEmpty(t *testing.T) {
	t.Parallel()
	app := New("test-app").
		RegisterTool(NewTool("plain").
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil }))
	raw := getMetaRaw(t, app.Mux())
	if _, exists := raw["capabilities"]; exists {
		t.Errorf("raw JSON should omit capabilities, got keys: %v", keysOf(raw))
	}
}

// TestApp_RegisterTool_MixedBindings 验证多个工具中只要有任一带 binding，
// capabilities.ui.enabled 即声明；不带 binding 的工具的 _meta 字段省略。
func TestApp_RegisterTool_MixedBindings(t *testing.T) {
	t.Parallel()
	app := New("mixed").
		RegisterTool(NewTool("plain").
			WithDescription("无 UI 工具").
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil })).
		RegisterTool(NewTool("review").
			WithDescription("UI 工具").
			WithToolUI(kstypes.ToolUIBinding{Widget: "ks://widgets/diff-review@v1"}).
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil }))

	resp := getMeta(t, app.Mux())
	if resp.Capabilities == nil || resp.Capabilities.UI == nil || !resp.Capabilities.UI.Enabled {
		t.Fatalf("Capabilities.UI.Enabled should be true with at least one binding, got: %+v", resp.Capabilities)
	}
	if len(resp.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(resp.Tools))
	}
	// 找到两个 tool 分别核对
	for _, tl := range resp.Tools {
		switch tl.Name {
		case "plain":
			if tl.Meta != nil {
				t.Errorf("plain tool Meta should be nil, got: %+v", tl.Meta)
			}
		case "review":
			if tl.Meta == nil || tl.Meta.UI == nil || tl.Meta.UI.Widget != "ks://widgets/diff-review@v1" {
				t.Errorf("review tool meta.UI: got %+v", tl.Meta)
			}
		default:
			t.Errorf("unexpected tool: %s", tl.Name)
		}
	}
}

// TestApp_RegisterTool_NoTool_NoCapabilities 验证完全无工具时也不出 capabilities。
func TestApp_RegisterTool_NoTool_NoCapabilities(t *testing.T) {
	t.Parallel()
	app := New("empty-tool")
	resp := getMeta(t, app.Mux())
	if resp.Capabilities != nil {
		t.Errorf("Capabilities should be nil with no tools, got: %+v", resp.Capabilities)
	}
}

// TestApp_RegisterTool_DuplicateName_Panics 验证重复注册同名 tool panic（与既有 Tool() 一致）。
func TestApp_RegisterTool_DuplicateName_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望重复注册 tool panic")
		}
	}()
	New("dup-app").
		RegisterTool(NewTool("same").
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil })).
		RegisterTool(NewTool("same").
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil }))
}

// TestApp_RegisterTool_DuplicateWithLegacyTool_Panics 验证 RegisterTool 与既有 Tool()
// 共用同一份 toolNames 注册表（混用时也能检出重名）。
func TestApp_RegisterTool_DuplicateWithLegacyTool_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 Tool 与 RegisterTool 同名时 panic")
		}
	}()
	New("dup-app").
		Tool("legacy", "x", func(ctx context.Context, p map[string]any) (any, error) { return nil, nil }).
		RegisterTool(NewTool("legacy").
			WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil }))
}

// TestToolBuilder_WithAnnotations 验证 WithAnnotations 把 MCP 2025-03-26
// annotations 写入 ToolDef.Annotations，供 mcpToolDefs 透传到 tools/list 响应。
func TestToolBuilder_WithAnnotations(t *testing.T) {
	t.Parallel()
	b := NewTool("demo").
		WithDescription("demo").
		WithAnnotations(map[string]any{"readOnlyHint": true}).
		WithHandler(func(ctx context.Context, p map[string]any) (any, error) {
			return "ok", nil
		})
	def := b.Build()
	if got, want := def.Annotations["readOnlyHint"], true; got != want {
		t.Errorf("readOnlyHint = %v, want %v", got, want)
	}
}

// keysOf 从 map[string]any 提取 keys 的 helper 在 config_handler_test.go 已定义，
// 本文件直接复用同 package 测试文件内的函数。
