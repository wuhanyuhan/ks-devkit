package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/tester"
	kstypes "github.com/wuhanyuhan/ks-types"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "本地验证应用（静态检查 + 运行时探测）",
	RunE:  runTest,
}

func init() {
	rootCmd.AddCommand(testCmd)
	testCmd.Flags().Bool("skip-runtime", false, "跳过运行时探测，仅执行静态检查")
	testCmd.Flags().Int("port", 18080, "运行时探测使用的端口")
	testCmd.Flags().Duration("timeout", 30_000_000_000, "运行时探测超时时间") // 30s
}

func runTest(cmd *cobra.Command, args []string) error {
	projectDir := "."
	totalFailed := 0

	// 阶段 1：静态检查
	staticResults := tester.RunStaticChecks(projectDir)
	totalFailed += tester.Report("静态检查", staticResults)

	skipRuntime, _ := cmd.Flags().GetBool("skip-runtime")
	if skipRuntime {
		return reportFinal(totalFailed)
	}

	// 静态检查有硬失败（manifest 解析失败）则不继续运行时探测
	for _, r := range staticResults {
		if !r.Passed && (r.Name == "manifest.yaml" || r.Name == "manifest 格式") {
			fmt.Fprintln(os.Stderr, "\n静态检查存在关键错误，跳过运行时探测")
			return reportFinal(totalFailed)
		}
	}

	// 解析 manifest 获取工具列表（用于一致性对比）
	var manifestTools []string
	manifestData, _ := os.ReadFile(filepath.Join(projectDir, "manifest.yaml"))
	if manifest, err := kstypes.ParseAppSpec(manifestData); err == nil {
		// manifest 本身不直接声明工具列表，工具信息在运行时通过 /mcp/tools/list 获取
		// 此处传空列表，跳过一致性对比（manifest 没有 tools 字段的标准定义）
		_ = manifest
	}

	// 阶段 2：运行时探测
	port, _ := cmd.Flags().GetInt("port")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	runtimeResults, err := tester.StartAndProbe(projectDir, port, timeout, manifestTools)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n运行时探测失败: %v\n", err)
		totalFailed++
	} else {
		totalFailed += tester.Report("运行时探测", runtimeResults)
	}

	return reportFinal(totalFailed)
}

func reportFinal(failed int) error {
	fmt.Println()
	if failed > 0 {
		return fmt.Errorf("检测完成，%d 项未通过", failed)
	}
	fmt.Println("✓ 所有检查通过")
	return nil
}
