package ksapp

import (
	"strings"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/meta"
)

// TestValidateConfigConsistency_Panics 不合法组合启动期终检应 panic。
func TestValidateConfigConsistency_Panics(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(a *App)
		wantInMsg string
	}{
		{"fullpage_iframe", func(a *App) {
			a.DeclareNav(meta.NavDecl{Label: "X", Category: "应用", OpenMode: "fullpage"})
			a.SetConfigMode("iframe")
		}, "非法"},
		{"fullpage_schema", func(a *App) {
			a.DeclareNav(meta.NavDecl{Label: "X", Category: "应用", OpenMode: "fullpage"})
			a.SetConfigMode("schema")
		}, "非法"},
		{"absent_schema", func(a *App) {
			a.SetConfigMode("schema")
		}, "未声明 nav"},
		{"dialog_iframe_no_ui", func(a *App) {
			a.DeclareNav(meta.NavDecl{Label: "X", Category: "配置", OpenMode: "dialog"})
			a.SetConfigMode("iframe")
		}, "config_ui"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%s 应 panic", c.name)
				}
				msg, _ := r.(string)
				if !strings.Contains(msg, c.wantInMsg) {
					t.Errorf("panic 消息不含 %q: %q", c.wantInMsg, msg)
				}
			}()
			a := New("demo")
			c.setup(a)
			a.validateConfigConsistency()
		})
	}
}

// TestValidateConfigConsistency_Legal 合法组合不 panic。
func TestValidateConfigConsistency_Legal(t *testing.T) {
	cases := []struct {
		name  string
		setup func(a *App)
	}{
		{"dialog_schema", func(a *App) {
			a.DeclareNav(meta.NavDecl{Label: "X", Category: "配置", OpenMode: "dialog"})
			a.SetConfigMode("schema")
		}},
		{"fullpage_none", func(a *App) {
			a.DeclareNav(meta.NavDecl{Label: "X", Category: "应用", OpenMode: "fullpage"})
			a.SetConfigMode("none")
		}},
		{"dialog_iframe_with_ui", func(a *App) {
			a.DeclareNav(meta.NavDecl{Label: "X", Category: "配置", OpenMode: "dialog"})
			a.SetConfigMode("iframe")
			a.DeclareConfigUI(meta.ConfigUIInfo{Enabled: true, URL: "/config-ui/"})
		}},
		{"pure_tool_no_nav", func(a *App) {}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s 合法组合不应 panic，得 %v", c.name, r)
				}
			}()
			a := New("demo")
			c.setup(a)
			a.validateConfigConsistency()
		})
	}
}
