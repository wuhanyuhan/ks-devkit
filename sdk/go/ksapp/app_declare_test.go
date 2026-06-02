package ksapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/meta"
	kstypes "github.com/wuhanyuhan/ks-types"
)

// getMeta 发起 GET /meta 并解析为 kstypes.MetaResponse。
func getMeta(t *testing.T, h http.Handler) kstypes.MetaResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/meta", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /meta status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp kstypes.MetaResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析 /meta 响应失败: %v", err)
	}
	return resp
}

// getMetaRaw 返回 /meta 的原始 JSON map，用于断言字段省略。
func getMetaRaw(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/meta", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /meta status = %d", w.Code)
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析 /meta raw 失败: %v", err)
	}
	return raw
}

func TestApp_DeclareNav_ReflectedInMeta(t *testing.T) {
	t.Parallel()
	app := New("nav-app").DeclareNav(meta.NavDecl{
		Label:         "文档",
		Icon:          "file-text",
		Category:      "应用",
		Order:         5,
		OpenMode:      "fullpage",
		EntryPath:     "/",
		RequiredPerms: []string{"mcp.doc.view"},
	})
	resp := getMeta(t, app.Mux())

	if resp.Nav == nil {
		t.Fatal("Nav 应非 nil")
	}
	if resp.Nav.Label != "文档" {
		t.Errorf("Label = %q, 期望 文档", resp.Nav.Label)
	}
	if resp.Nav.Icon != "file-text" {
		t.Errorf("Icon = %q", resp.Nav.Icon)
	}
	if resp.Nav.Category != "应用" {
		t.Errorf("Category = %q", resp.Nav.Category)
	}
	if resp.Nav.Order != 5 {
		t.Errorf("Order = %d", resp.Nav.Order)
	}
	if resp.Nav.OpenMode != "fullpage" {
		t.Errorf("OpenMode = %q", resp.Nav.OpenMode)
	}
	if resp.Nav.EntryPath != "/" {
		t.Errorf("EntryPath = %q", resp.Nav.EntryPath)
	}
	if len(resp.Nav.RequiredPerms) != 1 || resp.Nav.RequiredPerms[0] != "mcp.doc.view" {
		t.Errorf("RequiredPerms = %v", resp.Nav.RequiredPerms)
	}
}

func TestApp_DeclarePermission_MultipleCalls(t *testing.T) {
	t.Parallel()
	app := New("perm-app").
		DeclarePermission(meta.PermissionDecl{Code: "mcp.doc.view", Label: "查看文档"}).
		DeclarePermission(meta.PermissionDecl{Code: "mcp.doc.edit", Label: "编辑文档", DefaultRoles: []string{"admin"}})
	resp := getMeta(t, app.Mux())

	if len(resp.Permissions) != 2 {
		t.Fatalf("Permissions 长度 = %d, 期望 2", len(resp.Permissions))
	}
	if resp.Permissions[0].Code != "mcp.doc.view" {
		t.Errorf("[0].Code = %q", resp.Permissions[0].Code)
	}
	if resp.Permissions[1].Code != "mcp.doc.edit" {
		t.Errorf("[1].Code = %q", resp.Permissions[1].Code)
	}
	if len(resp.Permissions[1].DefaultRoles) != 1 || resp.Permissions[1].DefaultRoles[0] != "admin" {
		t.Errorf("[1].DefaultRoles = %v", resp.Permissions[1].DefaultRoles)
	}
}

func TestApp_SetConfigMode_Valid(t *testing.T) {
	t.Parallel()
	cases := []string{"schema", "iframe", "none"}
	for _, mode := range cases {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			app := New("cfg-app").SetConfigMode(mode)
			resp := getMeta(t, app.Mux())
			if resp.ConfigMode != mode {
				t.Errorf("ConfigMode = %q, 期望 %q", resp.ConfigMode, mode)
			}
		})
	}
}

func TestApp_SetConfigMode_Invalid_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望非法 config_mode panic")
		}
	}()
	New("cfg-app").SetConfigMode("invalid-mode")
}

func TestApp_SetConfigStatus_Valid(t *testing.T) {
	t.Parallel()
	cases := []string{"unconfigured", "via_frontend", "via_cli", "mixed"}
	for _, status := range cases {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			app := New("cs-app").SetConfigStatus(status)
			resp := getMeta(t, app.Mux())
			if resp.ConfigStatus != status {
				t.Errorf("ConfigStatus = %q, 期望 %q", resp.ConfigStatus, status)
			}
		})
	}
}

func TestApp_SetConfigStatus_Invalid_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望非法 config_status panic")
		}
	}()
	New("cs-app").SetConfigStatus("bogus_state")
}

func TestApp_SetProtocolVersion_DefaultAndOverride(t *testing.T) {
	t.Parallel()
	// 默认值："1.0"
	appDefault := New("pv-default")
	respDefault := getMeta(t, appDefault.Mux())
	if respDefault.ProtocolVersion != "1.0" {
		t.Errorf("默认 ProtocolVersion = %q, 期望 1.0", respDefault.ProtocolVersion)
	}

	// 覆盖成 "2.0"
	appCustom := New("pv-custom").SetProtocolVersion("2.0")
	respCustom := getMeta(t, appCustom.Mux())
	if respCustom.ProtocolVersion != "2.0" {
		t.Errorf("覆盖后 ProtocolVersion = %q, 期望 2.0", respCustom.ProtocolVersion)
	}
}

func TestApp_DeclareConfigUI_ReflectedInMeta(t *testing.T) {
	t.Parallel()
	app := New("ui-app").
		SetConfigMode("iframe").
		DeclareConfigUI(meta.ConfigUIInfo{
			Enabled: true,
			URL:     "/config-ui/",
		})
	resp := getMeta(t, app.Mux())

	if resp.ConfigUI == nil {
		t.Fatal("ConfigUI 应非 nil")
	}
	if !resp.ConfigUI.Enabled {
		t.Error("ConfigUI.Enabled 应为 true")
	}
	if resp.ConfigUI.URL != "/config-ui/" {
		t.Errorf("ConfigUI.URL = %q", resp.ConfigUI.URL)
	}
	if resp.ConfigMode != "iframe" {
		t.Errorf("ConfigMode = %q, 期望 iframe", resp.ConfigMode)
	}
}

func TestApp_ConfigUIMiddleware_NonKeystoneMode_IsNoop(t *testing.T) {
	t.Parallel()
	// authMode=none 时 ConfigUIMiddleware 返回 pass-through，无 Authorization 仍 200。
	app := New("noauth-cfg-app") // 默认 AuthModeNone
	mw := app.ConfigUIMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest("GET", "/config-any", nil) // 无 Authorization
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("no-op middleware 应 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, 期望 ok", rec.Body.String())
	}
}

func TestApp_EmptyDeclare_OmitsFieldsInMeta(t *testing.T) {
	t.Parallel()
	// 不调任何 Declare/Set 方法，除默认 protocol_version 外新字段都应从 JSON 中省略。
	app := New("empty-app")
	raw := getMetaRaw(t, app.Mux())

	for _, field := range []string{"nav", "permissions", "config_mode", "config_ui", "config_status"} {
		if _, exists := raw[field]; exists {
			t.Errorf("字段 %q 不应出现在 /meta JSON 中（未声明），但存在: %+v", field, raw[field])
		}
	}
	// protocol_version 默认值 "1.0" 应存在
	if pv, ok := raw["protocol_version"].(string); !ok || pv != "1.0" {
		t.Errorf("protocol_version 默认应为 \"1.0\", got %v", raw["protocol_version"])
	}
}

// TestApp_DeclareNav_Override 额外验证重复调 DeclareNav 以最后一次为准（对齐 TS SDK 语义）。
func TestApp_DeclareNav_Override(t *testing.T) {
	t.Parallel()
	app := New("nav-override").
		DeclareNav(meta.NavDecl{Label: "旧", Category: "应用", OpenMode: "dialog"}).
		DeclareNav(meta.NavDecl{Label: "新", Category: "工具", OpenMode: "fullpage"})
	resp := getMeta(t, app.Mux())

	if resp.Nav == nil {
		t.Fatal("Nav 应非 nil")
	}
	if resp.Nav.Label != "新" {
		t.Errorf("Label = %q, 期望 新（最后一次覆盖）", resp.Nav.Label)
	}
	if resp.Nav.OpenMode != "fullpage" {
		t.Errorf("OpenMode = %q, 期望 fullpage", resp.Nav.OpenMode)
	}
}
