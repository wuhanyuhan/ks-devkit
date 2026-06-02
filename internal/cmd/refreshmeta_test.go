package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestRefreshMeta_IdempotentReSync 用有状态 mock 模拟 keystone install 对已存在 app 的
// 非幂等拒绝（ErrAppAlreadyInstalled）。若 refresh-meta 不先 uninstall，第二次 install 必撞错——
// 故本测试强制 uninstall→install 的重同步设计。
func TestRefreshMeta_IdempotentReSync(t *testing.T) {
	installed := false // mock keystone 安装状态
	installs, uninstalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/login":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": "t"}})
		case "/v1/admin/apps/install":
			if installed {
				json.NewEncoder(w).Encode(map[string]any{"code": 40009, "message": "app already installed"})
				return
			}
			installed = true
			installs++
			json.NewEncoder(w).Encode(map[string]any{"code": 0})
		case "/v1/admin/apps/uninstall":
			uninstalls++
			installed = false
			json.NewEncoder(w).Encode(map[string]any{"code": 0})
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	os.WriteFile("manifest.yaml", []byte("id: my-app\nname: my-app\nversion: 0.1.0\ntype: app\nruntime:\n  mode: container\n"), 0644)

	cmd := refreshMetaCmd
	cmd.Flags().Set("keystone-url", srv.URL)
	// 第一次（未安装）：uninstall best-effort + install。
	if err := runRefreshMeta(cmd, nil); err != nil {
		t.Fatalf("refresh-meta #1: %v", err)
	}
	// 第二次（已安装）：必须先 uninstall 再 install，否则撞 already-installed。
	if err := runRefreshMeta(cmd, nil); err != nil {
		t.Fatalf("refresh-meta #2（幂等重同步应成功）: %v", err)
	}
	if installs != 2 {
		t.Errorf("install 调用 %d 次 want 2（每次 refresh 重装一次）", installs)
	}
	if uninstalls < 1 {
		t.Errorf("refresh-meta 应先 uninstall 再 install（uninstalls=%d）", uninstalls)
	}
}
