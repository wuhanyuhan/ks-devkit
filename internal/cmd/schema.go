package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/resources"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "输出 manifest JSON Schema（喂 IDE 校验/补全）",
	Long:  "打印内嵌的 manifest JSON Schema（由 ks-types 字段注释生成）。--write 写入当前项目 .ks/manifest.schema.json。",
	RunE:  runSchema,
}

func init() {
	rootCmd.AddCommand(schemaCmd)
	schemaCmd.Flags().Bool("write", false, "写入当前项目 .ks/manifest.schema.json（供 IDE 引用）")
}

func runSchema(cmd *cobra.Command, _ []string) error {
	data, err := resources.SchemaFS.ReadFile("schema/manifest.schema.json")
	if err != nil {
		return fmt.Errorf("读取内嵌 schema 失败: %w", err)
	}
	write, _ := cmd.Flags().GetBool("write")
	if !write {
		fmt.Print(string(data))
		return nil
	}
	if err := os.MkdirAll(".ks", 0755); err != nil {
		return err
	}
	out := filepath.Join(".ks", "manifest.schema.json")
	if err := os.WriteFile(out, data, 0644); err != nil {
		return err
	}
	fmt.Printf("✓ 已写出 %s（manifest.yaml 顶部 modeline 已指向它）\n", out)
	return nil
}
