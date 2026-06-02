package ksapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMountStaticRoot_RequiresConfigModeNone 未设 config_mode="none" 时调用 panic。
func TestMountStaticRoot_RequiresConfigModeNone(t *testing.T) {
	cases := []struct {
		name       string
		configMode string // "" 表示不调 SetConfigMode
	}{
		{"unset", ""},
		{"schema", "schema"},
		{"iframe", "iframe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("configMode=%q 时 MountStaticRoot 应 panic", c.configMode)
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("panic value 不是 string: %T %v", r, r)
				}
				if !strings.Contains(msg, "MountStaticRoot 只能在 config_mode=\"none\"") {
					t.Errorf("panic 消息不包含期望片段: %q", msg)
				}
			}()
			app := New("demo")
			if c.configMode != "" {
				app.SetConfigMode(c.configMode)
			}
			app.MountStaticRoot("/tmp/dist")
		})
	}
}

// TestMountStaticRoot_PanicsOnRepeat 重复调用 panic。
func TestMountStaticRoot_PanicsOnRepeat(t *testing.T) {
	app := New("demo").SetConfigMode("none")
	app.MountStaticRoot("/tmp/dist1")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("重复 MountStaticRoot 应 panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value 不是 string: %T %v", r, r)
		}
		if !strings.Contains(msg, "MountStaticRoot 已被调用") {
			t.Errorf("panic 消息不包含期望片段: %q", msg)
		}
	}()
	app.MountStaticRoot("/tmp/dist2")
}

// TestMountStaticRoot_ServesRootIndex 端到端：/ 返 index.html、/assets/main.js 返 js、
// 未命中路径 SPA fallback 到 index.html。
func TestMountStaticRoot_ServesRootIndex(t *testing.T) {
	tmpDir := t.TempDir()
	indexContent := "<!doctype html><title>SPA</title>"
	jsContent := "console.log('hi');"
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.Mkdir(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "main.js"), []byte(jsContent), 0644); err != nil {
		t.Fatal(err)
	}

	app := New("demo").SetConfigMode("none")
	app.MountStaticRoot(tmpDir)

	ts := httptest.NewServer(app.Mux())
	defer ts.Close()

	cases := []struct {
		name     string
		path     string
		wantBody string
		contains bool // wantBody 是否用 contains 匹配
		wantCT   string
	}{
		{"root index", "/", indexContent, true, ""},
		{"asset js", "/assets/main.js", jsContent, true, ""},
		{"spa fallback", "/unknown-spa-route", indexContent, true, ""},
		{"spa fallback nested", "/users/42", indexContent, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + c.path)
			if err != nil {
				t.Fatalf("GET %s: %v", c.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("GET %s status = %d, want 200", c.path, resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if c.contains {
				if !strings.Contains(string(body), c.wantBody) {
					t.Errorf("GET %s body = %q, 期望包含 %q", c.path, string(body), c.wantBody)
				}
			}
		})
	}

	// /healthz 不应被根挂载吞掉
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("GET /healthz status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("GET /healthz Content-Type = %q, 期望 application/json", ct)
	}

	// /meta 同样不应被吞
	resp2, err := http.Get(ts.URL + "/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("GET /meta status = %d, want 200", resp2.StatusCode)
	}
	ct2 := resp2.Header.Get("Content-Type")
	if !strings.Contains(ct2, "application/json") {
		t.Errorf("GET /meta Content-Type = %q, 期望 application/json", ct2)
	}
}

// TestMountStaticRoot_CoexistsWithCustomRoutes Handle("GET /api/data", ...) 与根挂载共存，
// /api/data 命中自定义 handler（Go 1.22+ ServeMux 方法+路径最长匹配优先于 "/"）。
func TestMountStaticRoot_CoexistsWithCustomRoutes(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("INDEX"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New("demo").SetConfigMode("none")
	app.HandleFunc("GET /api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"from-custom-handler"}`))
	})
	app.Tool("noop", "noop", func(ctx context.Context, p map[string]any) (any, error) {
		return nil, nil
	})
	app.MountStaticRoot(tmpDir)

	ts := httptest.NewServer(app.Mux())
	defer ts.Close()

	// /api/data 命中自定义 handler
	resp, err := http.Get(ts.URL + "/api/data")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "from-custom-handler") {
		t.Errorf("GET /api/data body = %q，期望命中自定义 handler", string(body))
	}

	// / 命中静态根
	resp2, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), "INDEX") {
		t.Errorf("GET / body = %q，期望命中静态根 index.html", string(body2))
	}
}
