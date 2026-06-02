package tester

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunStaticChecks_ValidManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := `id: test-app
name: Test
version: 1.0.0
type: app
permissions:
  network:
    level: restricted
`
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644)

	results := RunStaticChecks(dir)
	for _, r := range results {
		if !r.Passed {
			t.Errorf("check %q failed: %s", r.Name, r.Message)
		}
	}
}

func TestRunStaticChecks_MissingManifest(t *testing.T) {
	dir := t.TempDir()
	results := RunStaticChecks(dir)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Passed {
		t.Error("expected manifest check to fail")
	}
}

func TestRunStaticChecks_InvalidPermission(t *testing.T) {
	dir := t.TempDir()
	manifest := `id: test-app
name: Test
version: 1.0.0
type: app
permissions:
  network:
    level: invalid_level
`
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644)

	results := RunStaticChecks(dir)
	permFailed := false
	for _, r := range results {
		if r.Name == "权限声明" && !r.Passed {
			permFailed = true
		}
	}
	if !permFailed {
		t.Error("expected permission check to fail")
	}
}

func TestRunStaticChecks_HighRiskWarning(t *testing.T) {
	dir := t.TempDir()
	manifest := `id: test-app
name: Test
version: 1.0.0
type: app
permissions:
  filesystem:
    level: full
`
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644)

	results := RunStaticChecks(dir)
	hasWarning := false
	for _, r := range results {
		if r.Name == "高风险权限" && !r.Passed {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected high-risk warning for filesystem:full")
	}
}
