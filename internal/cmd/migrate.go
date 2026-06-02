package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/manifest"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate [manifest.yaml]",
	Short: "把旧 manifest 迁移到 clean-break 声明（默认 dry-run，打印迁移后内容）",
	Long: "把旧 manifest（type:service/extension/assistant、带前缀 canonical_name、" +
		"cost_hint/input_nl 等废字段、旧 dependencies）机械迁移到 clean-break 声明形态。\n" +
		"默认 dry-run 打印到 stdout（差异交 git diff 看）；--write 写回文件。\n" +
		"squad 仓必须显式 --type=squad（store.team 自动判定不可靠）。",
	RunE: runMigrate,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().Bool("write", false, "写回文件（默认只打印到 stdout）")
	migrateCmd.Flags().String("type", "", "强制目标 type（squad 仓必须传 squad）")
}

func runMigrate(cmd *cobra.Command, args []string) error {
	path := "manifest.yaml"
	if len(args) > 0 {
		path = args[0]
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	typeOverride, _ := cmd.Flags().GetString("type")
	out, rep, err := manifest.Migrate(raw, manifest.MigrateOptions{TypeOverride: typeOverride})
	if err != nil {
		return err
	}
	write, _ := cmd.Flags().GetBool("write")
	if write {
		if err := os.WriteFile(path, out, 0644); err != nil {
			return err
		}
		fmt.Printf("✓ 已写回 %s\n", path)
	} else {
		// dry-run：打印迁移后内容（差异交给 git diff 看）
		fmt.Print(string(out))
		fmt.Fprintln(os.Stderr, "（dry-run；--write 写回）")
	}
	printMigrateWarnings(rep)
	// install.yaml 的 config_fields/secret_fields 是 SDK 代码迁移面，migrate 不自动转，
	// 检测到就提示人工用 config-schema 收敛。
	if dir := filepath.Dir(path); fileHasKeys(filepath.Join(dir, "install.yaml"), "config_fields", "secret_fields") {
		fmt.Fprintln(os.Stderr, "⚠ 检测到 install.yaml 的 config_fields/secret_fields——配置职责已收敛到 config-schema，需在 app 代码里用 SDK 声明 config-schema 后删除（见 docs/migration-checklist.md）")
	}
	return nil
}

// printMigrateWarnings 把需人工跟进项打到 stderr（不影响 stdout 的迁移内容）。
func printMigrateWarnings(rep *manifest.MigrateReport) {
	if rep == nil {
		return
	}
	for _, w := range rep.Warnings {
		fmt.Fprintf(os.Stderr, "⚠ %s\n", w)
	}
}

// fileHasKeys 报告 path 文件是否包含任一 key（简单子串匹配，用于 install.yaml 配置职责检测；
// 仅作 advisory 告警，宽松匹配可接受。文件不存在/读不了返回 false）。
func fileHasKeys(path string, keys ...string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := string(raw)
	for _, k := range keys {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}
