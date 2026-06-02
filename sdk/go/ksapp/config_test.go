package ksapp

import (
	"os"
	"testing"
)

func TestLoadAppConfig_Default(t *testing.T) {
	os.Unsetenv("KS_APP_PORT")
	cfg := loadAppConfig()
	if cfg.Port != 8080 {
		t.Errorf("port: %d", cfg.Port)
	}
}

func TestLoadAppConfig_FromEnv(t *testing.T) {
	t.Setenv("KS_APP_PORT", "9999")
	cfg := loadAppConfig()
	if cfg.Port != 9999 {
		t.Errorf("port: %d", cfg.Port)
	}
}

func TestLoadAppConfig_InvalidEnv(t *testing.T) {
	t.Setenv("KS_APP_PORT", "not-a-number")
	cfg := loadAppConfig()
	if cfg.Port != 8080 {
		t.Errorf("port on invalid env: %d (should fall back to 8080)", cfg.Port)
	}
}
