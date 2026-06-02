package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestValidateProjectName(t *testing.T) {
	valid := []string{"my-app", "app_1", "App2", "a"}
	for _, n := range valid {
		if err := validateProjectName(n); err != nil {
			t.Errorf("expected %q valid, got: %v", n, err)
		}
	}

	invalid := []string{"", ".", "..", ".hidden", "foo/bar", "..\\evil", "../../etc"}
	for _, n := range invalid {
		if err := validateProjectName(n); err == nil {
			t.Errorf("expected %q invalid", n)
		}
	}
}

func TestRenderTemplate_UnsupportedTemplate(t *testing.T) {
	outDir := t.TempDir()
	err := renderTemplate("nonexistent-template", outDir, map[string]string{"Name": "test"})
	if err == nil {
		t.Fatal("期望返回错误，但得到 nil")
	}
	if !strings.Contains(err.Error(), "不支持的模板") {
		t.Errorf("期望错误消息包含 '不支持的模板'，实际: %v", err)
	}
}

// 注：旧的 TestRenderTemplate_AssistantTemplate* / *ServiceTemplate* 已随 clean-break
// 删除——它们断言 AppTypeAssistant / Mount.Assistant 等 v0.30.0 已砍的符号，且测的是
// 即将在 S3 P1 删除的旧模板（assistant/service-*）。新四类型模板的生成测试见 P1 Task 3-7。

func TestInit_AppGo_GeneratesCleanBreakManifest(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := initCmd
	cmd.Flags().Set("type", "app")
	cmd.Flags().Set("lang", "go")
	if err := runInit(cmd, []string{"my-app"}); err != nil {
		t.Fatalf("init app-go 失败: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "my-app", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// 1) 可被 clean-break 契约解析 + 通过校验（ks build/ks test 走的就是 Validate）
	spec, err := kstypes.ParseAppSpec(raw)
	if err != nil {
		t.Fatalf("生成的 manifest 解析失败: %v", err)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("生成的 manifest 未通过 clean-break 校验: %v", err)
	}
	if spec.Type != kstypes.AppTypeApp {
		t.Errorf("type=%q，want app", spec.Type)
	}
	// 2) provides 用裸名（无前缀点）
	caps := spec.Provides.Capabilities
	if len(caps) == 0 || strings.Contains(caps[0].Name, ".") {
		t.Errorf("provides[0].name=%q，应为裸名", caps[0].Name)
	}
	// 3) 必含 auth.mode=keystone_jwks
	if spec.Auth.Mode != kstypes.AuthModeKeystoneJWKS {
		t.Errorf("auth.mode=%q，want keystone_jwks", spec.Auth.Mode)
	}
	// 4) 无被砍字段（字符串级防回归）
	for _, banned := range []string{"cost_hint", "typical_latency_ms", "intent_summary", "input_nl", "runtime:\n  port", "requires_approval"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("manifest 含已砍字段/形态: %q", banned)
		}
	}
}

func TestInit_AppPython_And_AppTS_Parse(t *testing.T) {
	for _, lang := range []string{"python", "ts"} {
		dir := t.TempDir()
		t.Chdir(dir)
		cmd := initCmd
		cmd.Flags().Set("type", "app")
		cmd.Flags().Set("lang", lang)
		if err := runInit(cmd, []string{"my-app"}); err != nil {
			t.Fatalf("init app-%s 失败: %v", lang, err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "my-app", "manifest.yaml"))
		spec, err := kstypes.ParseAppSpec(raw)
		if err != nil {
			t.Fatalf("app-%s manifest 解析失败: %v", lang, err)
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("app-%s manifest 未过校验: %v", lang, err)
		}
		// 语言专属入口存在
		entry := map[string]string{"python": "main.py", "ts": "index.ts"}[lang]
		if _, err := os.Stat(filepath.Join(dir, "my-app", entry)); err != nil {
			t.Fatalf("app-%s 缺少入口 %s", lang, entry)
		}
	}
}

func TestInit_Squad_DerivesExpertTeamAndRequiresRoster(t *testing.T) {
	for _, lang := range []string{"go", "python", "ts"} {
		dir := t.TempDir()
		t.Chdir(dir)
		cmd := initCmd
		cmd.Flags().Set("type", "squad")
		cmd.Flags().Set("lang", lang)
		if err := runInit(cmd, []string{"my-squad"}); err != nil {
			t.Fatalf("init squad-%s 失败: %v", lang, err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "my-squad", "manifest.yaml"))
		spec, err := kstypes.ParseAppSpec(raw)
		if err != nil {
			t.Fatalf("squad-%s manifest 解析失败: %v", lang, err)
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("squad-%s manifest 未过校验: %v", lang, err)
		}
		if spec.Type != kstypes.AppTypeSquad {
			t.Errorf("type=%q want squad", spec.Type)
		}
		// squad 必带团队名册（expert_team 信任展示；key/name 为 StoreTeamMemberSpec 必填）
		if spec.Store.Team == nil || len(spec.Store.Team.Members) < 1 {
			t.Errorf("squad 必须声明 store.team.members≥1")
		}
	}
}

func TestInit_AgentAndSkill_Langless(t *testing.T) {
	for _, typ := range []string{"agent", "skill"} {
		dir := t.TempDir()
		t.Chdir(dir)
		cmd := initCmd
		cmd.Flags().Set("type", typ)
		cmd.Flags().Set("lang", "python") // 应被忽略
		if err := runInit(cmd, []string{"my-" + typ}); err != nil {
			t.Fatalf("init %s 失败: %v", typ, err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "my-"+typ, "manifest.yaml"))
		spec, err := kstypes.ParseAppSpec(raw)
		if err != nil {
			t.Fatalf("%s manifest 解析失败: %v", typ, err)
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("%s manifest 未过校验: %v", typ, err)
		}
		if string(spec.Type) != typ {
			t.Errorf("type=%q want %q", spec.Type, typ)
		}
		if spec.Runtime.Mode != kstypes.RuntimeModeNone {
			t.Errorf("%s runtime.mode 应为 none", typ)
		}
		// langless：无语言入口文件
		for _, banned := range []string{"main.go", "main.py", "index.ts"} {
			if _, err := os.Stat(filepath.Join(dir, "my-"+typ, banned)); err == nil {
				t.Errorf("%s 不应生成语言入口 %s", typ, banned)
			}
		}
	}
}

// TestInit_AllEightCombosBuildableManifest 覆盖性集成测试：8 组合各生成可解析 + 过校验的 manifest。
func TestInit_AllEightCombosBuildableManifest(t *testing.T) {
	combos := []struct{ typ, lang string }{
		{"app", "go"}, {"app", "python"}, {"app", "ts"},
		{"squad", "go"}, {"squad", "python"}, {"squad", "ts"},
		{"agent", ""}, {"skill", ""},
	}
	for _, c := range combos {
		dir := t.TempDir()
		t.Chdir(dir)
		cmd := initCmd
		cmd.Flags().Set("type", c.typ)
		if c.lang != "" {
			cmd.Flags().Set("lang", c.lang)
		}
		name := "x-" + c.typ
		if err := runInit(cmd, []string{name}); err != nil {
			t.Fatalf("init %s/%s 失败: %v", c.typ, c.lang, err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, name, "manifest.yaml"))
		spec, err := kstypes.ParseAppSpec(raw)
		if err != nil {
			t.Fatalf("%s/%s manifest 不可解析: %v", c.typ, c.lang, err)
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("%s/%s manifest 未过校验: %v", c.typ, c.lang, err)
		}
	}
}

func TestInit_WiresSchemaForIDE(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := initCmd
	cmd.Flags().Set("type", "app")
	cmd.Flags().Set("lang", "go")
	if err := runInit(cmd, []string{"my-app"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "my-app")
	// 1) manifest 顶部含 modeline（模板已带，验证存在）
	raw, _ := os.ReadFile(filepath.Join(base, "manifest.yaml"))
	if !strings.Contains(string(raw), "yaml-language-server: $schema=.ks/manifest.schema.json") {
		t.Error("manifest 缺 schema modeline")
	}
	// 2) .ks/manifest.schema.json 已写出
	if _, err := os.Stat(filepath.Join(base, ".ks", "manifest.schema.json")); err != nil {
		t.Errorf("缺 .ks/manifest.schema.json: %v", err)
	}
	// 3) .vscode/settings.json 含 yaml.schemas 映射
	vs, err := os.ReadFile(filepath.Join(base, ".vscode", "settings.json"))
	if err != nil || !strings.Contains(string(vs), "yaml.schemas") {
		t.Errorf(".vscode/settings.json 缺 yaml.schemas 映射: %v", err)
	}
}
