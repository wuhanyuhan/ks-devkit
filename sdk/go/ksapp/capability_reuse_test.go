package ksapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeReuseManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// 情形①：tool_name 命中已有 app.Tool，无独立 handler → 复用（不新增 tool、不报错）。
func TestReuseExistingTool(t *testing.T) {
	mp := writeReuseManifest(t, `
id: ks-mcp-browser
provides:
  capabilities:
    - name: web_search
      execution_mode: sync
      backend: {kind: mcp_tool, tool_name: web_search}
`)
	app := New("ks-mcp-browser", WithManifest(mp))
	app.Tool("web_search", "原子工具", func(ctx context.Context, p map[string]any) (any, error) {
		return map[string]any{"hit": true}, nil
	})
	before := len(app.tools)
	if err := app.ensureCapabilityFinalized(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if len(app.tools) != before {
		t.Fatalf("reuse must NOT add a new tool: before=%d after=%d", before, len(app.tools))
	}
}

// 情形②：无对应 tool，有独立 handler → 生成新 tool（现状行为）。
func TestGenerateNewToolWhenHandlerProvided(t *testing.T) {
	mp := writeReuseManifest(t, `
id: ks-mcp-x
provides:
  capabilities:
    - name: gen
      execution_mode: sync
      backend: {kind: mcp_tool, tool_name: gen_tool}
`)
	app := New("ks-mcp-x", WithManifest(mp))
	app.RegisterCapability("gen", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return map[string]any{}, nil
	})
	if err := app.ensureCapabilityFinalized(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if _, ok := app.toolNames["gen_tool"]; !ok {
		t.Fatalf("expected generated tool gen_tool")
	}
}

// 情形③：既有独立 handler 又撞已有 app.Tool → 报错（真冲突）。
func TestConflictHandlerAndTool(t *testing.T) {
	mp := writeReuseManifest(t, `
id: ks-mcp-x
provides:
  capabilities:
    - name: dup
      execution_mode: sync
      backend: {kind: mcp_tool, tool_name: dup_tool}
`)
	app := New("ks-mcp-x", WithManifest(mp))
	app.Tool("dup_tool", "已有", func(ctx context.Context, p map[string]any) (any, error) { return nil, nil })
	app.RegisterCapability("dup", func(ctx CapabilityContext, args map[string]any) (any, error) { return nil, nil })
	if err := app.ensureCapabilityFinalized(); err == nil {
		t.Fatalf("expected conflict error")
	}
}

// 情形④：无 tool 无 handler → 报错（无承载）。
func TestNoBackendNoHandlerErrors(t *testing.T) {
	mp := writeReuseManifest(t, `
id: ks-mcp-x
provides:
  capabilities:
    - name: orphan
      execution_mode: sync
      backend: {kind: mcp_tool, tool_name: missing_tool}
`)
	app := New("ks-mcp-x", WithManifest(mp))
	if err := app.ensureCapabilityFinalized(); err == nil {
		t.Fatalf("expected no-backend error")
	}
}
