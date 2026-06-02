package schemagen

import (
	"os"
	"testing"
)

// TestSchemaNotDrifted 锁死"committed schema 必须由当前 ks-types 注释生成、不手改"。
// 重新生成 → 与仓内 committed 文件逐字节比对。漂移即红（CI 守门）。
func TestSchemaNotDrifted(t *testing.T) {
	committed, err := os.ReadFile("../resources/schema/manifest.schema.json")
	if err != nil {
		t.Fatalf("读 committed schema: %v", err)
	}
	regen, err := Generate("github.com/wuhanyuhan/ks-types", "AppSpec")
	if err != nil {
		t.Fatalf("重新生成: %v", err)
	}
	// committed 末尾有换行（genschema 追加），比对时对齐
	if string(committed) != string(regen)+"\n" {
		t.Fatal("manifest.schema.json 与 ks-types 注释漂移——请重跑 `go generate ./internal/resources/` 并提交")
	}
}
