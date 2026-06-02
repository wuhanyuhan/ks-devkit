package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/internal/resources"
)

func TestCheckCommand_Go(t *testing.T) {
	c := checkCommand("Go", "go", "version")
	// go 一定存在于测试环境
	if !c.passed {
		t.Errorf("go version 应通过: %s", c.message)
	}
	if c.message == "" {
		t.Error("message 不应为空")
	}
}

func TestCheckCommand_NonExistent(t *testing.T) {
	c := checkCommand("Fake", "nonexistent-binary-12345")
	if c.passed {
		t.Error("不存在的命令应失败")
	}
	if c.message == "" {
		t.Error("失败时 message 应包含原因")
	}
}

func TestCheckCommand_MultilineOutput(t *testing.T) {
	// 用一个输出多行的命令，确认 checkCommand 只保留首行
	c := checkCommand("MultiLine", "printf", "line1\nline2\n")
	if !c.passed {
		t.Fatalf("printf 应执行成功")
	}
	if c.message != "line1" {
		t.Errorf("应只保留首行，实际 %q", c.message)
	}
}

func TestCheckTemplatesDir_WithEnv(t *testing.T) {
	// 设置 KS_TEMPLATES_DIR 指向一个临时目录，验证 env 分支
	tmp := t.TempDir()
	templates := filepath.Join(tmp, "templates")
	if err := os.MkdirAll(templates, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KS_TEMPLATES_DIR", templates)

	c := checkTemplatesDir()
	if !c.passed {
		t.Errorf("设置了 KS_TEMPLATES_DIR 应通过: %s", c.message)
	}
}

func TestCheckTemplatesDir_EmbeddedAlwaysPasses(t *testing.T) {
	// 即使设置了不存在的 KS_TEMPLATES_DIR，内嵌模板检查仍应通过
	t.Setenv("KS_TEMPLATES_DIR", "/nonexistent/path")
	c := checkTemplatesDir()
	if !c.passed {
		t.Errorf("内嵌模板应始终存在: %s", c.message)
	}
}

func TestCheckCredentials_Missing(t *testing.T) {
	// 让 HOME 指向一个空目录，保证 credentials.json 不存在
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := checkCredentials()
	if c.passed {
		t.Error("无凭证文件时应失败")
	}
}

func TestCheckCredentials_Corrupt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credDir := filepath.Join(home, ".ks")
	if err := os.MkdirAll(credDir, 0700); err != nil {
		t.Fatal(err)
	}
	credFile := filepath.Join(credDir, "credentials.json")
	// 写入故意损坏的 JSON
	if err := os.WriteFile(credFile, []byte("{ this is not valid json"), 0600); err != nil {
		t.Fatal(err)
	}
	c := checkCredentials()
	if c.passed {
		t.Error("损坏的凭证文件不应通过")
	}
	if !strings.Contains(c.message, "凭证文件损坏") {
		t.Errorf("message 应包含 '凭证文件损坏'，实际: %s", c.message)
	}
	if strings.Contains(c.message, "未登录") {
		t.Errorf("损坏的文件不应提示未登录，实际: %s", c.message)
	}
}

func TestCheckCredentials_Present(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// 写入最小合法凭证
	credDir := filepath.Join(home, ".ks")
	if err := os.MkdirAll(credDir, 0700); err != nil {
		t.Fatal(err)
	}
	credFile := filepath.Join(credDir, "credentials.json")
	content := `{"access_token":"abc","refresh_token":"def","email":"user@example.com"}`
	if err := os.WriteFile(credFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	c := checkCredentials()
	if !c.passed {
		t.Errorf("合法凭证应通过: %s", c.message)
	}
	if !strings.Contains(c.message, "user@example.com") {
		t.Errorf("message 应包含 email: %s", c.message)
	}
}

func TestDoctor_IncludesNodeAndBunChecks(t *testing.T) {
	// runDoctor 组装的 checks 必须含 Node 与 bun 两项（TS 模板编译/运行所需）。
	names := checkNames(collectChecks())
	for _, want := range []string{"Node", "bun"} {
		if !names[want] {
			t.Fatalf("doctor 缺少检查项 %q，现有：%v", want, names)
		}
	}
}

func checkNames(cs []doctorCheck) map[string]bool {
	m := map[string]bool{}
	for _, c := range cs {
		m[c.name] = true
	}
	return m
}

func TestDoctor_TemplatesCountIsEight(t *testing.T) {
	c := checkTemplatesDir()
	if !c.passed {
		t.Fatalf("模板目录自检失败: %s", c.message)
	}
	// 8 模板目录：app/squad×{go,python,ts} + agent/skill langless。
	entries, _ := fs.ReadDir(resources.TemplatesFS, "templates")
	if len(entries) != 8 {
		t.Errorf("模板目录数 %d，want 8", len(entries))
	}
}
