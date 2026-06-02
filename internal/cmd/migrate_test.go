package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateCmd_WriteRenamesType(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(p, []byte("id: x\nname: X\nversion: 0.1.0\ntype: assistant\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := migrateCmd
	if err := cmd.Flags().Set("write", "true"); err != nil {
		t.Fatal(err)
	}
	defer cmd.Flags().Set("write", "false") // 复位全局 flag，避免泄漏到其他 cmd 测试
	if err := runMigrate(cmd, []string{p}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "type: agent") {
		t.Errorf("type 未迁移：%s", raw)
	}
}

func TestFileHasKeys(t *testing.T) {
	dir := t.TempDir()
	withCfg := filepath.Join(dir, "install.yaml")
	if err := os.WriteFile(withCfg, []byte("config_fields:\n  - name: KS_CHUNK_SIZE\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !fileHasKeys(withCfg, "config_fields", "secret_fields") {
		t.Errorf("含 config_fields 应检测到")
	}

	noCfg := filepath.Join(dir, "other.yaml")
	if err := os.WriteFile(noCfg, []byte("foo: bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if fileHasKeys(noCfg, "config_fields", "secret_fields") {
		t.Errorf("不含 config_fields/secret_fields 不应误报")
	}

	if fileHasKeys(filepath.Join(dir, "nope.yaml"), "config_fields") {
		t.Errorf("不存在的文件应返回 false")
	}
}
