package build

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuild_ValidProject(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(
		"id: test-app\nname: Test\nversion: 1.0.0\ntype: app\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)

	result, err := Build(dir, filepath.Join(dir, "dist"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.AppID != "test-app" {
		t.Errorf("app_id: %q", result.AppID)
	}
	if result.Version != "1.0.0" {
		t.Errorf("version: %q", result.Version)
	}
	if result.Checksum == "" {
		t.Error("empty checksum")
	}
	if _, err := os.Stat(result.TarballPath); err != nil {
		t.Errorf("tarball not found: %v", err)
	}

	// 关键：验证 tarball 内容非空，并包含 manifest.yaml 和 main.go
	// 这是为了避免 createTarball 的根目录跳过 bug 造成静默空包
	f, _ := os.Open(result.TarballPath)
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := map[string]bool{}
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		found[h.Name] = true
	}
	if !found["manifest.yaml"] {
		t.Error("tarball missing manifest.yaml")
	}
	if !found["main.go"] {
		t.Error("tarball missing main.go")
	}
}

func TestBuild_MissingManifest(t *testing.T) {
	dir := t.TempDir()
	_, err := Build(dir, filepath.Join(dir, "dist"))
	if err == nil {
		t.Error("expected error for missing manifest")
	}
}

func TestBuild_InvalidManifest(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(
		"name: NoID\nversion: 1.0.0\ntype: app\n"), 0644)
	_, err := Build(dir, filepath.Join(dir, "dist"))
	if err == nil {
		t.Error("expected error for manifest missing id")
	}
}

func TestBuild_InvalidPermissionLevel(t *testing.T) {
	dir := t.TempDir()
	manifest := `id: test-app
name: Test
version: 1.0.0
type: app
permissions:
  network:
    level: full_access
`
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)

	_, err := Build(dir, filepath.Join(dir, "dist"))
	if err == nil {
		t.Fatal("期望权限校验失败，但构建成功了")
	}
	if !strings.Contains(err.Error(), "权限声明无效") {
		t.Errorf("期望错误包含 '权限声明无效'，实际: %v", err)
	}
}

func TestBuild_ValidPermissions(t *testing.T) {
	dir := t.TempDir()
	manifest := `id: test-app
name: Test
version: 1.0.0
type: app
permissions:
  network:
    level: restricted
  filesystem:
    level: read_scoped
`
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)

	result, err := Build(dir, filepath.Join(dir, "dist"))
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if result.AppID != "test-app" {
		t.Errorf("app_id: %q", result.AppID)
	}
}

// TestBuild_SkipsHiddenFilesAndDist 锁定安全不变量：隐藏文件（.env / .git/）
// 和 dist/ 输出目录必须从 tarball 中排除，避免敏感信息泄漏或循环打包。
func TestBuild_SkipsHiddenFilesAndDist(t *testing.T) {
	dir := t.TempDir()

	// 合法的 manifest + 源码
	_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(
		"id: test-app\nname: Test\nversion: 1.0.0\ntype: app\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)

	// 应被排除：隐藏文件
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=xyz\n"), 0644)

	// 应被排除：.git 目录及其下内容
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0644)

	// 应被排除：dist/ 下上一次构建的残留
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "dist", "stale.tar.gz"), []byte("stale"), 0644)

	result, err := Build(dir, filepath.Join(dir, "dist"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// 打开生成的 tarball 并收集所有 entry 名
	f, err := os.Open(result.TarballPath)
	if err != nil {
		t.Fatalf("open tarball: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := map[string]bool{}
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		entries[h.Name] = true
	}

	// 不得出现的路径
	forbidden := []string{
		".env",
		".git/HEAD",
		".git",
		"dist/stale.tar.gz",
		"dist",
	}
	for _, name := range forbidden {
		if entries[name] {
			t.Errorf("tarball 不应包含 %q", name)
		}
	}

	// 必须存在的路径（保证 tarball 不是空包）
	if !entries["manifest.yaml"] {
		t.Error("tarball missing manifest.yaml")
	}
	if !entries["main.go"] {
		t.Error("tarball missing main.go")
	}
}
