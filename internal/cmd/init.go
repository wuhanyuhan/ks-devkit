package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/resources"
	"github.com/wuhanyuhan/ks-devkit/internal/template"
)

var initCmd = &cobra.Command{
	Use:   "init <name>",
	Short: "创建新的 Keystone 应用项目",
	Args:  cobra.ExactArgs(1),
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("type", "app", "应用类型 (app/squad/agent/skill)")
	initCmd.Flags().String("lang", "go", "编程语言 (go/python/ts)，agent/skill 忽略")
	initCmd.Flags().String("publisher", "", "Publisher slug")
}

var validTypes = map[string]bool{"app": true, "squad": true, "agent": true, "skill": true}
var validLangs = map[string]bool{"go": true, "python": true, "ts": true}

// isLangless 判断 type 是否 langless：agent/skill 运行在 keystone 内（runtime none），
// 无独立进程/语言，模板不带语言后缀、忽略 --lang。
func isLangless(appType string) bool { return appType == "agent" || appType == "skill" }

// templateName 把 (type,lang) 映射到内嵌模板目录名。
func templateName(appType, lang string) string {
	if isLangless(appType) {
		return appType // agent / skill
	}
	return appType + "-" + lang // app-go / squad-python / ...
}

// langSuffix 仅用于成功提示展示：非 langless 返回 "/<lang>"，langless 返回空。
func langSuffix(appType, lang string) string {
	if isLangless(appType) {
		return ""
	}
	return "/" + lang
}

func runInit(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateProjectName(name); err != nil {
		return err
	}

	appType, _ := cmd.Flags().GetString("type")
	lang, _ := cmd.Flags().GetString("lang")
	publisher, _ := cmd.Flags().GetString("publisher")

	// ① 失败点即文档：非法值报错带允许清单。
	if !validTypes[appType] {
		return fmt.Errorf("不支持的 type %q（允许 app|squad|agent|skill）", appType)
	}
	if isLangless(appType) {
		if lang != "go" { // 用户显式传了非默认 lang
			fmt.Printf("提示：%s 类型运行在 keystone 内（无独立进程），忽略 --lang=%s\n", appType, lang)
		}
	} else if !validLangs[lang] {
		return fmt.Errorf("不支持的 lang %q（允许 go|python|ts）", lang)
	}

	outputDir := filepath.Join(".", name)
	if _, err := os.Stat(outputDir); err == nil {
		return fmt.Errorf("目录 %s 已存在", name)
	}

	data := map[string]string{
		"AppID":     name,
		"Name":      name,
		"Summary":   fmt.Sprintf("%s 应用", name),
		"Publisher": publisher,
		"Type":      appType,
		"Language":  lang,
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	if err := renderTemplate(templateName(appType, lang), outputDir, data); err != nil {
		_ = os.RemoveAll(outputDir)
		return fmt.Errorf("渲染模板失败: %w", err)
	}

	// 写 IDE schema 接线（.ks/manifest.schema.json + .vscode/settings.json）。
	// 失败不致命：仅提示（可手动 ks schema --write）。
	if err := writeIDEWiring(outputDir); err != nil {
		fmt.Printf("提示：IDE schema 接线未完成（可手动 ks schema --write）：%v\n", err)
	}

	fmt.Printf("✓ 项目 %s 已创建（type=%s%s）\n", name, appType, langSuffix(appType, lang))
	fmt.Println("  下一步：")
	if !isLangless(appType) {
		fmt.Printf("    cd %s && ks dev && ks register\n", name)
	}
	fmt.Println("  不确定怎么填？见 docs/decision-guide.md（怎么选类型 / tool vs capability）")
	return nil
}

// renderTemplate 渲染模板。优先使用环境变量指定的目录，否则使用内嵌模板。
func renderTemplate(templateName, outputDir string, data map[string]string) error {
	if envDir := os.Getenv("KS_TEMPLATES_DIR"); envDir != "" {
		templateDir := filepath.Join(envDir, templateName)
		if _, err := os.Stat(templateDir); err == nil {
			return template.Render(templateDir, outputDir, data)
		}
	}

	// 内嵌模板路径：templates/<templateName>
	templateDir := "templates/" + templateName
	if _, err := fs.Stat(resources.TemplatesFS, templateDir); err != nil {
		return fmt.Errorf("不支持的模板: %s", templateName)
	}

	return template.RenderFromFS(resources.TemplatesFS, templateDir, outputDir, data)
}

// writeIDEWiring 把内嵌 schema 写到项目 .ks/ 并配 .vscode/settings.json，
// 让 VS Code Red Hat YAML 扩展对 manifest.yaml 做校验 + 补全。
func writeIDEWiring(outputDir string) error {
	data, err := resources.SchemaFS.ReadFile("schema/manifest.schema.json")
	if err != nil {
		return err
	}
	ksDir := filepath.Join(outputDir, ".ks")
	if err := os.MkdirAll(ksDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ksDir, "manifest.schema.json"), data, 0644); err != nil {
		return err
	}
	vsDir := filepath.Join(outputDir, ".vscode")
	if err := os.MkdirAll(vsDir, 0755); err != nil {
		return err
	}
	settings := `{
  "yaml.schemas": {
    "./.ks/manifest.schema.json": "manifest.yaml"
  }
}
`
	return os.WriteFile(filepath.Join(vsDir, "settings.json"), []byte(settings), 0644)
}

func validateProjectName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\") ||
		strings.HasPrefix(name, ".") {
		return fmt.Errorf("项目名非法: %q（仅允许字母、数字、下划线、中划线）", name)
	}
	return nil
}
