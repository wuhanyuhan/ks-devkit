package ksapp

import (
	"context"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestNewTool_FluentChain(t *testing.T) {
	t.Parallel()
	handler := func(ctx context.Context, params map[string]any) (any, error) {
		return nil, nil
	}
	b := NewTool("review_draft").
		WithDescription("审阅指定 draft").
		WithInputSchema(map[string]any{"type": "object"}).
		WithToolUI(kstypes.ToolUIBinding{Widget: "ks://widgets/diff-review@v1"}).
		WithHandler(handler)

	if b == nil {
		t.Fatal("NewTool returned nil")
	}
	def := b.Build()
	if def.Name != "review_draft" {
		t.Errorf("Name: got %q, want review_draft", def.Name)
	}
	if def.Description != "审阅指定 draft" {
		t.Errorf("Description: got %q", def.Description)
	}
	if def.UIBinding == nil || def.UIBinding.Widget != "ks://widgets/diff-review@v1" {
		t.Errorf("UIBinding: got %+v", def.UIBinding)
	}
	if def.Handler == nil {
		t.Error("Handler is nil")
	}
	if def.InputSchema == nil {
		t.Error("InputSchema is nil")
	}
}

func TestNewTool_OptionalUIBinding(t *testing.T) {
	t.Parallel()
	b := NewTool("list_drafts").
		WithDescription("x").
		WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil })
	def := b.Build()
	if def.UIBinding != nil {
		t.Errorf("UIBinding should be nil, got %+v", def.UIBinding)
	}
}

// TestNewTool_WithToolUISandboxHints 验证 SandboxHints 透传。
func TestNewTool_WithToolUISandboxHints(t *testing.T) {
	t.Parallel()
	b := NewTool("x").
		WithToolUI(kstypes.ToolUIBinding{
			Widget:       "ks://widgets/diff-review@v1",
			SandboxHints: []string{"allow-scripts", "allow-same-origin"},
		}).
		WithHandler(func(ctx context.Context, p map[string]any) (any, error) { return nil, nil })
	def := b.Build()
	if def.UIBinding == nil {
		t.Fatal("UIBinding nil")
	}
	if len(def.UIBinding.SandboxHints) != 2 {
		t.Errorf("SandboxHints len = %d, want 2", len(def.UIBinding.SandboxHints))
	}
}
