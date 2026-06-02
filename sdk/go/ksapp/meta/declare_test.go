package meta_test

import (
	"encoding/json"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/meta"
)

func TestDeclare_MinimalJSON(t *testing.T) {
	t.Parallel()

	r := meta.Declare("demo", "0.1.0")
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}

	if got["name"] != "demo" {
		t.Errorf("name 应为 demo，实际 %v", got["name"])
	}
	if got["version"] != "0.1.0" {
		t.Errorf("version 应为 0.1.0，实际 %v", got["version"])
	}
	if got["auth_mode"] != "keystone_jwks" {
		t.Errorf("auth_mode 默认应为 keystone_jwks，实际 %v", got["auth_mode"])
	}
	if got["protocol_version"] != "1.0" {
		t.Errorf("protocol_version 默认应为 1.0，实际 %v", got["protocol_version"])
	}
	// 未设置的 omitempty 字段不应出现
	if _, ok := got["nav"]; ok {
		t.Errorf("未设置时 nav 不应出现在 JSON 中")
	}
	if _, ok := got["permissions"]; ok {
		t.Errorf("未设置时 permissions 不应出现在 JSON 中")
	}
	if _, ok := got["config_mode"]; ok {
		t.Errorf("未设置时 config_mode 不应出现在 JSON 中")
	}
	if _, ok := got["config_status"]; ok {
		t.Errorf("未设置时 config_status 不应出现在 JSON 中")
	}
	if _, ok := got["tools"]; ok {
		t.Errorf("未设置时 tools 不应出现在 JSON 中")
	}
}

func TestDeclare_WithPermissionsAndTools(t *testing.T) {
	t.Parallel()

	r := meta.Declare("writer", "1.2.3",
		meta.WithNav(meta.NavDecl{
			Label:         "写手",
			Icon:          "pen-tool",
			Category:      "应用",
			Order:         10,
			OpenMode:      "fullpage",
			EntryPath:     "/",
			RequiredPerms: []string{"mcp.writer.view"},
		}),
		meta.WithPermissions(
			meta.PermissionDecl{Code: "mcp.writer.view", Label: "查看写手", DefaultRoles: []string{"admin"}},
			meta.PermissionDecl{Code: "mcp.writer.edit", Label: "编辑写手"},
		),
		meta.WithConfigMode("iframe"),
		meta.WithConfigStatus("via_frontend"),
		meta.WithTools(
			map[string]any{"name": "draft", "description": "起草一篇初稿"},
		),
	)

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}

	if got["config_mode"] != "iframe" {
		t.Errorf("config_mode 应为 iframe，实际 %v", got["config_mode"])
	}
	if got["config_status"] != "via_frontend" {
		t.Errorf("config_status 应为 via_frontend，实际 %v", got["config_status"])
	}

	nav, ok := got["nav"].(map[string]any)
	if !ok {
		t.Fatalf("nav 应为 object，实际 %T", got["nav"])
	}
	if nav["label"] != "写手" {
		t.Errorf("nav.label 应为 写手，实际 %v", nav["label"])
	}
	if nav["open_mode"] != "fullpage" {
		t.Errorf("nav.open_mode 应为 fullpage，实际 %v", nav["open_mode"])
	}
	perms, ok := nav["required_perms"].([]any)
	if !ok || len(perms) != 1 || perms[0] != "mcp.writer.view" {
		t.Errorf("nav.required_perms 格式错误: %v", nav["required_perms"])
	}

	permsArr, ok := got["permissions"].([]any)
	if !ok || len(permsArr) != 2 {
		t.Fatalf("permissions 应为长度 2 的数组，实际 %v", got["permissions"])
	}
	p0, _ := permsArr[0].(map[string]any)
	if p0["code"] != "mcp.writer.view" {
		t.Errorf("permissions[0].code 应为 mcp.writer.view，实际 %v", p0["code"])
	}
	// 第二个没设置 default_roles，应因 omitempty 不出现
	p1, _ := permsArr[1].(map[string]any)
	if _, hasRoles := p1["default_roles"]; hasRoles {
		t.Errorf("permissions[1].default_roles 未设置时不应出现")
	}

	tools, ok := got["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools 应为长度 1 的数组，实际 %v", got["tools"])
	}
	tool0, _ := tools[0].(map[string]any)
	if tool0["name"] != "draft" {
		t.Errorf("tools[0].name 应为 draft，实际 %v", tool0["name"])
	}
}

func TestDeclare_IframeConfigUI(t *testing.T) {
	t.Parallel()

	r := meta.Declare("doc", "1.0.0",
		meta.WithConfigMode("iframe"),
		meta.WithConfigUI(meta.ConfigUIInfo{Enabled: true, URL: "/admin/ui/doc/"}),
	)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}

	if got["config_mode"] != "iframe" {
		t.Errorf("config_mode 应为 iframe，实际 %v", got["config_mode"])
	}

	cfgUI, ok := got["config_ui"].(map[string]any)
	if !ok {
		t.Fatalf("config_ui 应为 object，实际 %T", got["config_ui"])
	}
	if cfgUI["url"] != "/admin/ui/doc/" {
		t.Errorf("config_ui.url 应为 /admin/ui/doc/，实际 %v", cfgUI["url"])
	}
	if cfgUI["enabled"] != true {
		t.Errorf("config_ui.enabled 应为 true，实际 %v", cfgUI["enabled"])
	}

	// 未设置 WithConfigUI 时 config_ui 应因 omitempty 不出现（对比验证）
	r2 := meta.Declare("doc", "1.0.0")
	b2, err := json.Marshal(r2)
	if err != nil {
		t.Fatalf("marshal 基线失败: %v", err)
	}
	var got2 map[string]any
	if err := json.Unmarshal(b2, &got2); err != nil {
		t.Fatalf("unmarshal 基线失败: %v", err)
	}
	if _, ok := got2["config_ui"]; ok {
		t.Errorf("未调用 WithConfigUI 时 config_ui 不应出现在 JSON 中")
	}
}
