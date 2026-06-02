package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindComposeFile_YAMLExtension(t *testing.T) {
	dir := t.TempDir()
	// 在临时目录里放 docker-compose.yaml
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yaml"), []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 切换到临时目录后调用
	restore := chdir(t, dir)
	defer restore()

	name := findLocalComposeFile()
	if name != "docker-compose.yaml" {
		t.Errorf("findLocalComposeFile: %q", name)
	}
}

func TestFindComposeFile_YMLExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	restore := chdir(t, dir)
	defer restore()

	name := findLocalComposeFile()
	if name != "docker-compose.yml" {
		t.Errorf("findLocalComposeFile: %q", name)
	}
}

func TestFindComposeFile_YAMLPreferred(t *testing.T) {
	dir := t.TempDir()
	// 同时存在 yaml 和 yml 时，yaml 优先
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yaml"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	restore := chdir(t, dir)
	defer restore()

	name := findLocalComposeFile()
	if name != "docker-compose.yaml" {
		t.Errorf("preference wrong: %q", name)
	}
}

func TestFindComposeFile_NotFound(t *testing.T) {
	dir := t.TempDir()

	restore := chdir(t, dir)
	defer restore()

	name := findLocalComposeFile()
	if name != "" {
		t.Errorf("expected empty, got %q", name)
	}
}

// chdir 切换工作目录到 dir，返回一个恢复函数。
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		_ = os.Chdir(prev)
	}
}
