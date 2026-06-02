package ksapp

import "testing"

// TestWireMCPToolBackend_InputSchemaTransfer 验证 manifest 声明的 input_schema
// 在 wireMCPToolBackend 后正确出现在 ToolDef.InputSchema（回归测试）。
func TestWireMCPToolBackend_InputSchemaTransfer(t *testing.T) {
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
    - name: echo
      execution_mode: sync
      backend:
        kind: mcp_tool
        tool_name: echo
      input_schema:
        type: object
        properties:
          message:
            type: string
            description: 回显内容
        required: [message]
`)
	app := New("ks-mcp-x", WithManifest(manifest))
	app.RegisterCapability("echo", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return args, nil
	})
	if err := app.ensureCapabilityFinalized(); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	var def *ToolDef
	for i := range app.tools {
		if app.tools[i].Name == "echo" {
			def = &app.tools[i]
			break
		}
	}
	if def == nil {
		t.Fatal("tool echo not registered")
	}
	if def.InputSchema == nil {
		t.Fatal("ToolDef.InputSchema is nil — 未透传 manifest 的 input_schema")
	}
	if def.InputSchema["type"] != "object" {
		t.Errorf("InputSchema.type = %v, want object", def.InputSchema["type"])
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema.properties not map, got %T", def.InputSchema["properties"])
	}
	if _, ok := props["message"]; !ok {
		t.Error("InputSchema.properties.message missing")
	}
}

// TestWireMCPToolBackend_NilInputSchemaStaysNil 验证 manifest 未声明 input_schema
// 时 ToolDef.InputSchema 保持 nil（不会被空 map 误填）。
func TestWireMCPToolBackend_NilInputSchemaStaysNil(t *testing.T) {
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
    - name: bare
      execution_mode: sync
      backend:
        kind: mcp_tool
        tool_name: bare
`)
	app := New("ks-mcp-x", WithManifest(manifest))
	app.RegisterCapability("bare", func(ctx CapabilityContext, args map[string]any) (any, error) {
		return nil, nil
	})
	if err := app.ensureCapabilityFinalized(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	for _, td := range app.tools {
		if td.Name == "bare" {
			if td.InputSchema != nil {
				t.Errorf("ToolDef.InputSchema should stay nil when manifest declares none, got %v", td.InputSchema)
			}
			return
		}
	}
	t.Fatal("tool bare not registered")
}
