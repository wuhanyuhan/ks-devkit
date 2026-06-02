package ksapp

import (
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestNew_NoOptions_DefaultAuthNone(t *testing.T) {
	app := New("demo")
	if app.authMode != kstypes.AuthModeNone {
		t.Errorf("默认 authMode 应为 none, got %q", app.authMode)
	}
}

func TestNew_WithKeystoneAuth(t *testing.T) {
	t.Setenv("KEYSTONE_JWKS_URL", "https://example.com/.well-known/jwks.json")
	app := New("demo", WithKeystoneAuth())
	if app.authMode != kstypes.AuthModeKeystoneJWKS {
		t.Errorf("WithKeystoneAuth 应置 authMode=keystone_jwks, got %q", app.authMode)
	}
	if app.jwksURL == "" {
		t.Errorf("jwksURL 应从 env 读取")
	}
}

func TestNew_WithoutAuth(t *testing.T) {
	app := New("demo", WithKeystoneAuth(), WithoutAuth())
	if app.authMode != kstypes.AuthModeNone {
		t.Errorf("WithoutAuth 应覆盖 WithKeystoneAuth，got %q", app.authMode)
	}
}

func TestNew_WithVersion(t *testing.T) {
	app := New("demo", WithVersion("2.1.0"))
	if app.version != "2.1.0" {
		t.Errorf("version: got %q", app.version)
	}
}
