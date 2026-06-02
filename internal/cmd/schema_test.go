package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSchemaCmd_WriteToProject(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := schemaCmd
	cmd.Flags().Set("write", "true")
	if err := runSchema(cmd, nil); err != nil {
		t.Fatalf("ks schema --write 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ks", "manifest.schema.json")); err != nil {
		t.Fatalf("未写出 .ks/manifest.schema.json: %v", err)
	}
}
