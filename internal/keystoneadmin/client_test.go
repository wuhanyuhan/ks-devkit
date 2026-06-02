package keystoneadmin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_LoginInstallUninstall(t *testing.T) {
	var gotAuth, gotInstallBody, gotUninstallQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/login":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": "tok-123"}})
		case "/v1/admin/apps/install":
			gotAuth = r.Header.Get("Authorization")
			b, _ := io.ReadAll(r.Body)
			gotInstallBody = string(b)
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"app_id": "my-app"}})
		case "/v1/admin/apps/uninstall":
			// keystone 从 query 读 app_id（c.Query("app_id")），非 JSON body。
			gotUninstallQuery = r.URL.RawQuery
			json.NewEncoder(w).Encode(map[string]any{"code": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL)
	if err := c.Login("admin", "admin"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.token != "tok-123" {
		t.Fatalf("token 未保存：%q", c.token)
	}
	if err := c.InstallApp(InstallReq{AppID: "my-app", Version: "0.1.0", ExternalEndpoint: "http://host.docker.internal:8080/mcp"}); err != nil {
		t.Fatalf("InstallApp: %v", err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("install 未带 Bearer token：%q", gotAuth)
	}
	if !strings.Contains(gotInstallBody, `"external_endpoint":"http://host.docker.internal:8080/mcp"`) {
		t.Errorf("install body 缺 external_endpoint：%s", gotInstallBody)
	}
	if !strings.Contains(gotInstallBody, `"app_id":"my-app"`) {
		t.Errorf("install body 缺 app_id：%s", gotInstallBody)
	}
	if err := c.UninstallApp("my-app"); err != nil {
		t.Fatalf("UninstallApp: %v", err)
	}
	if gotUninstallQuery != "app_id=my-app" {
		t.Errorf("uninstall 应把 app_id 放 query（keystone c.Query），实际 query=%q", gotUninstallQuery)
	}
}
