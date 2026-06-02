package ksapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeCapManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRegisterCapabilityStoresEntry(t *testing.T) {
	app := New("ks-mcp-x")
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if _, ok := app.capabilities["ks-mcp-x.foo"]; !ok {
		t.Fatal("expected registry to contain ks-mcp-x.foo")
	}
}

func TestRegisterCapabilityDuplicatePanics(t *testing.T) {
	app := New("ks-mcp-x")
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return nil, nil
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return nil, nil
	})
}

func TestFinalizeRejectsCapabilityNotInManifest(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: sync
      backend:
        kind: mcp_tool
        tool_name: foo
`)
	app := New("ks-mcp-x", WithManifest(manifest))
	app.RegisterCapability("bar", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return nil, nil
	})
	err := app.finalizeCapabilities()
	if err == nil {
		t.Fatal("expected ManifestMismatch")
	}
	if !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("expected ErrManifestMismatch, got %v", err)
	}
}

func TestFinalizeAttachesBackendKindFromManifest(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: long_running
      timeout_ms: 300000
      backend:
        kind: mcp_tool
        tool_name: foo
`)
	app := New("ks-mcp-x", WithManifest(manifest))
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return nil, nil
	})
	if err := app.finalizeCapabilities(); err != nil {
		t.Fatal(err)
	}
	entry := app.capabilities["ks-mcp-x.foo"]
	if entry.BackendKind != "mcp_tool" {
		t.Fatalf("BackendKind = %q", entry.BackendKind)
	}
	if entry.BackendToolName != "foo" {
		t.Fatalf("BackendToolName = %q", entry.BackendToolName)
	}
	if entry.TimeoutMs != 300000 {
		t.Fatalf("TimeoutMs = %d", entry.TimeoutMs)
	}
	if entry.ExecutionMode != "long_running" {
		t.Fatalf("ExecutionMode = %q", entry.ExecutionMode)
	}
}

func TestWireMCPToolBackendAttachesTool(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: sync
      backend:
        kind: mcp_tool
        tool_name: foo
`)
	app := New("ks-mcp-x", WithManifest(manifest))
	called := false
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		called = true
		return map[string]any{"ok": true}, nil
	})
	if err := app.finalizeCapabilities(); err != nil {
		t.Fatal(err)
	}
	if err := app.wireMCPToolBackend(); err != nil {
		t.Fatal(err)
	}
	// 同名 tool 应已注册
	if _, ok := app.toolNames["foo"]; !ok {
		t.Fatal("expected tool 'foo' to be registered by wireMCPToolBackend")
	}
	// 找到包装后的 tool 调用之
	var toolHandler ToolHandler
	for _, td := range app.tools {
		if td.Name == "foo" {
			toolHandler = td.Handler
			break
		}
	}
	if toolHandler == nil {
		t.Fatal("registered tool not found in app.tools")
	}
	_, err := toolHandler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected capability handler invoked via wrapper tool")
	}
}

func TestWireMCPToolBackendCollisionWithExistingTool(t *testing.T) {
	manifest := writeCapManifest(t, `
id: ks-mcp-x
name: ks-mcp-x
version: 0.1.0
type: service
runtime:
  mode: container
  port: 8080
  image: "ks-mcp-x:0.1.0"
provides:
  capabilities:
    - name: foo
      execution_mode: sync
      backend:
        kind: mcp_tool
        tool_name: foo
`)
	app := New("ks-mcp-x", WithManifest(manifest))
	app.Tool("foo", "existing tool", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, nil
	})
	app.RegisterCapability("foo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return nil, nil
	})
	if err := app.finalizeCapabilities(); err != nil {
		t.Fatal(err)
	}
	if err := app.wireMCPToolBackend(); err == nil {
		t.Fatal("expected collision error with existing @app.tool")
	}
}

func TestRegisterCapabilityDerivesCanonicalFromBareName(t *testing.T) {
	app := New("ks-mcp-x")
	app.RegisterCapability("web_search", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	if _, ok := app.capabilities["ks-mcp-x.web_search"]; !ok {
		t.Fatalf("expected derived canonical key ks-mcp-x.web_search, got keys=%v", capKeysOf(app.capabilities))
	}
	if app.capabilities["ks-mcp-x.web_search"].CanonicalName != "ks-mcp-x.web_search" {
		t.Fatalf("entry.CanonicalName=%q", app.capabilities["ks-mcp-x.web_search"].CanonicalName)
	}
}

func capKeysOf(m map[string]*capabilityEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFinalizeMatchesManifestByDerivedCanonical(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(`
id: ks-mcp-x
provides:
  capabilities:
    - name: web_search
      execution_mode: sync
      backend:
        kind: mcp_tool
        tool_name: web_search
`), 0o644); err != nil {
		t.Fatal(err)
	}
	app := New("ks-mcp-x", WithManifest(manifestPath))
	app.RegisterCapability("web_search", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return nil, nil
	})
	if err := app.finalizeCapabilities(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	entry := app.capabilities["ks-mcp-x.web_search"]
	if entry.BackendKind != "mcp_tool" || entry.BackendToolName != "web_search" {
		t.Fatalf("manifest meta not injected: %+v", entry)
	}
}

// TestCallCapabilityKeepsFullName 锁调用方不对称契约：caller 侧 CallCapability 传全名，
// 绝不被去前缀派生逻辑波及（引用他人能力写全名，不可派生）。
func TestCallCapabilityKeepsFullName(t *testing.T) {
	app := New("ks-mcp-x")
	app.SetDispatcherClient(NewDispatcherClient("http://gw", "tk"))
	cc := app.CallCapability("ks-mcp-other.generate")
	if cc.canonicalName != "ks-mcp-other.generate" {
		t.Fatalf("caller canonical must stay full name, got %q", cc.canonicalName)
	}
}
