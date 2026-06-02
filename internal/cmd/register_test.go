package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRegister_OrchestratesLoginAndInstall(t *testing.T) {
	var installedAppID, gotEndpoint string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/login":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": "t"}})
		case "/v1/admin/apps/install":
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			installedAppID, _ = req["app_id"].(string)
			gotEndpoint, _ = req["external_endpoint"].(string)
			json.NewEncoder(w).Encode(map[string]any{"code": 0})
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	os.WriteFile("manifest.yaml", []byte("id: my-app\nname: my-app\nversion: 0.1.0\ntype: app\nruntime:\n  mode: container\n"), 0644)

	cmd := registerCmd
	cmd.Flags().Set("keystone-url", srv.URL)
	if err := runRegister(cmd, nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	if installedAppID != "my-app" {
		t.Errorf("install app_id=%q want my-app", installedAppID)
	}
	if gotEndpoint != "http://host.docker.internal:8080/mcp" {
		t.Errorf("external_endpoint=%q", gotEndpoint)
	}
}
