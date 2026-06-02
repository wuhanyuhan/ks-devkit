package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// runGolden 跑一对 in/out fixture：Migrate(in) 应逐字节等于 out。
func runGolden(t *testing.T, name string, opts MigrateOptions) {
	t.Helper()
	in, err := os.ReadFile(filepath.Join("testdata", "migrate", name+".in.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "migrate", name+".out.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := Migrate(in, opts)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("golden %s 不匹配\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestMigrate_TypeRename(t *testing.T) {
	runGolden(t, "type_rename", MigrateOptions{}) // service→app, extension→app, assistant→agent
}

func TestMigrate_Provides(t *testing.T) {
	runGolden(t, "provides", MigrateOptions{})
}

func TestMigrate_AuthAndResources(t *testing.T) {
	runGolden(t, "auth_resources", MigrateOptions{})
}

func TestMigrate_DepsAndTopLevel(t *testing.T) {
	runGolden(t, "deps_toplevel", MigrateOptions{TypeOverride: "squad"})
}

func TestMigrate_AgentMount(t *testing.T) {
	runGolden(t, "agent_mount", MigrateOptions{})
}

// TestMigrate_FullDatabase 跑贴近真实 database 类 app manifest 的代表性切片，
// 一次覆盖 type/protection/runtime.port/带前缀 canonical_name/cost_hint/default_grant/
// allowed_callers/input_nl/output_nl/合并 description/managed_resources 去样板。
func TestMigrate_FullDatabase(t *testing.T) {
	runGolden(t, "full_database", MigrateOptions{})
}

// TestMigrate_Idempotent：迁移结果再迁一次应逐字节不变（mergeDescription 跳过已有 description、
// mapDelete 对已删字段 no-op、canonical_name 已是裸 name 等保证幂等）。
func TestMigrate_Idempotent(t *testing.T) {
	out, err := os.ReadFile(filepath.Join("testdata", "migrate", "full_database.out.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := Migrate(out, MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(out) {
		t.Errorf("迁移非幂等：再迁一次发生变化\n--- got ---\n%s\n--- want ---\n%s", got, out)
	}
}
