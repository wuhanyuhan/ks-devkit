package ksapp

import (
	"os"
	"path/filepath"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func writeManifest(t *testing.T, dir, authMode string) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.yaml")
	content := `id: demo
name: Demo
version: 1.0.0
type: service
runtime:
  mode: container
  port: 8080
  image: "demo:1.0.0"
mount:
  service:
    mcp_endpoint: "http://localhost:8080/mcp"
    auth_mode: ` + authMode + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveAuth_CodeOptionWins(t *testing.T) {
	dir := t.TempDir()
	mpath := writeManifest(t, dir, "none")

	t.Setenv("KEYSTONE_JWKS_URL", "https://x.test/jwks")
	t.Setenv("KS_APP_AUTH_MODE", "")

	app := New("demo", WithKeystoneAuth(), WithManifest(mpath))
	effective, jwksURL, err := resolveAuth(app)
	if err != nil {
		t.Fatal(err)
	}
	if effective != kstypes.AuthModeKeystoneJWKS {
		t.Errorf("code Option 应优先于 manifest, got %q", effective)
	}
	if jwksURL != "https://x.test/jwks" {
		t.Errorf("jwksURL: got %q", jwksURL)
	}
}

func TestResolveAuth_ManifestFallback(t *testing.T) {
	dir := t.TempDir()
	mpath := writeManifest(t, dir, "keystone_jwks")

	t.Setenv("KEYSTONE_JWKS_URL", "https://x.test/jwks")

	app := New("demo", WithManifest(mpath))
	// 此时 app.authMode 仍是默认 none（没有 Option 触发 keystone_jwks）
	effective, jwksURL, err := resolveAuth(app)
	if err != nil {
		t.Fatal(err)
	}
	if effective != kstypes.AuthModeKeystoneJWKS {
		t.Errorf("manifest 应 fallback 生效, got %q", effective)
	}
	if jwksURL != "https://x.test/jwks" {
		t.Errorf("jwksURL: got %q", jwksURL)
	}
}

func TestResolveAuth_ManifestMissingOK(t *testing.T) {
	// 无 manifest 且无 Option → default none
	app := New("demo", WithManifest(filepath.Join(t.TempDir(), "not_exist.yaml")))
	effective, _, err := resolveAuth(app)
	if err != nil {
		t.Fatal(err)
	}
	if effective != kstypes.AuthModeNone {
		t.Errorf("无 manifest 无 Option 应默认 none, got %q", effective)
	}
}

func TestResolveAuth_StrictPanicsOnMissingJWKSURL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("期望 panic (strict-by-default)")
		}
	}()
	t.Setenv("KEYSTONE_JWKS_URL", "")
	t.Setenv("KS_APP_AUTH_MODE", "")

	app := New("demo", WithKeystoneAuth())
	// WithKeystoneAuth 读到空 URL，resolveAuth 应 panic
	_, _, _ = resolveAuth(app)
}

func TestResolveAuth_InsecureEscapeHatch(t *testing.T) {
	t.Setenv("KEYSTONE_JWKS_URL", "")
	t.Setenv("KS_APP_AUTH_MODE", "insecure")

	app := New("demo", WithKeystoneAuth())
	effective, _, err := resolveAuth(app)
	if err != nil {
		t.Fatal(err)
	}
	if effective != kstypes.AuthModeNone {
		t.Errorf("KS_APP_AUTH_MODE=insecure 应降级为 none, got %q", effective)
	}
}

func TestResolveAuth_InvalidManifest(t *testing.T) {
	dir := t.TempDir()
	mpath := writeManifest(t, dir, "bogus_mode")

	app := New("demo", WithManifest(mpath))
	_, _, err := resolveAuth(app)
	if err == nil {
		t.Fatal("非法 manifest 应返回错误（不 panic）")
	}
}

func TestResolveAuth_ExtensionMountFallback(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(`
id: test-ext
name: test
version: "1.0"
type: extension
runtime:
  mode: container
  port: 9991
mount:
  extension:
    mcp_server_name: test-ext
    transport_type: streamable_http
    endpoint: http://localhost:9991/mcp
    auth_mode: keystone_jwks
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("KEYSTONE_JWKS_URL", "http://example.com/jwks")
	t.Setenv("KS_APP_AUTH_MODE", "")

	a := &App{
		authMode:     kstypes.AuthModeNone,
		manifestPath: manifestPath,
	}
	effective, jwksURL, err := resolveAuth(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if effective != kstypes.AuthModeKeystoneJWKS {
		t.Errorf("期望 effective=keystone_jwks，实际 %q", effective)
	}
	if jwksURL != "http://example.com/jwks" {
		t.Errorf("期望 jwksURL=http://example.com/jwks，实际 %q", jwksURL)
	}
}
