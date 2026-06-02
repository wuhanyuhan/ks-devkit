package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Default(t *testing.T) {
	t.Setenv("KS_HUB_URL", "") // 确保不被环境变量污染
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HubURL != DefaultHubURL {
		t.Errorf("hub_url: got %q, want %q", cfg.HubURL, DefaultHubURL)
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte("hub_url: http://localhost:9980\n"), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HubURL != "http://localhost:9980" {
		t.Errorf("hub_url: %q", cfg.HubURL)
	}
}
