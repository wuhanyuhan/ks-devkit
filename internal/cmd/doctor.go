package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/resources"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "检查开发环境是否就绪",
	RunE:  runDoctor,
}

func init() {
	// SilenceUsage: doctor 的 RunE 失败是预期的正常输出（区别于 init/login 等
	// 把 RunE 失败视为用法错误的命令），不应在 ✗ 列表后再 dump usage 噪音。
	doctorCmd.SilenceUsage = true
	rootCmd.AddCommand(doctorCmd)
}

// doctorCheck 单项环境诊断结果。
type doctorCheck struct {
	name    string
	passed  bool
	message string
}

// collectChecks 组装全部环境诊断项。抽成独立函数便于测试断言检查项集合，
// 也让 runDoctor 只负责渲染/汇总。
func collectChecks() []doctorCheck {
	return []doctorCheck{
		checkCommand("Go", "go", "version"),
		checkCommand("Python", "python3", "--version"),
		checkCommand("Node", "node", "--version"), // TS 模板 tsc/运行
		checkCommand("bun", "bun", "--version"),   // TS 模板首选 runtime
		// Docker: 仅检查 client 是否可用（不触发 daemon 查询，避免连接挂起）。
		// 完整 daemon 状态由 ks dev 启动流程负责。
		checkCommand("Docker", "docker", "version", "--format", "{{.Client.Version}}"),
		checkTemplatesDir(),
		checkCredentials(),
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	checks := collectChecks()

	failed := 0
	fmt.Println()
	fmt.Println("── 环境诊断 ──")
	for _, c := range checks {
		if c.passed {
			fmt.Printf("  ✓ %s: %s\n", c.name, c.message)
		} else {
			fmt.Printf("  ✗ %s: %s\n", c.name, c.message)
			failed++
		}
	}
	fmt.Println()

	if failed > 0 {
		return fmt.Errorf("%d 项检查未通过", failed)
	}
	fmt.Println("✓ 所有检查通过，开发环境就绪")
	return nil
}

// checkCommand 运行一个外部命令，成功返回其输出的首行（截断到 80 字符）。
func checkCommand(name string, command string, args ...string) doctorCheck {
	out, err := exec.Command(command, args...).CombinedOutput()
	if err != nil {
		return doctorCheck{name: name, passed: false, message: fmt.Sprintf("未安装或不可用: %v", err)}
	}
	version := strings.TrimSpace(string(out))
	// 只取第一行，避免多行输出污染列表
	if idx := strings.Index(version, "\n"); idx >= 0 {
		version = version[:idx]
	}
	// 截断到 80 字节（go/python/docker 输出均为 ASCII，无需处理 UTF-8 rune 边界）
	if len(version) > 80 {
		version = version[:80]
	}
	return doctorCheck{name: name, passed: true, message: version}
}

// checkTemplatesDir 检查内嵌模板是否可读。
func checkTemplatesDir() doctorCheck {
	entries, err := fs.ReadDir(resources.TemplatesFS, "templates")
	if err != nil {
		return doctorCheck{name: "模板目录", passed: false, message: fmt.Sprintf("内嵌模板不可读: %v", err)}
	}
	if len(entries) == 0 {
		return doctorCheck{name: "模板目录", passed: false, message: "内嵌模板为空"}
	}
	return doctorCheck{name: "模板目录", passed: true, message: fmt.Sprintf("内嵌（%d 个模板）", len(entries))}
}

// checkCredentials 尝试加载默认路径下的登录凭证。
// 区分两种失败：文件不存在（未登录）vs 文件存在但解析失败（凭证文件损坏），
// 后者避免用户在 ks login 后又被告知"未登录"而困惑。
func checkCredentials() doctorCheck {
	cred, err := auth.LoadCredentials(auth.DefaultCredentialsPath())
	if err != nil {
		// auth.LoadCredentials 用 fmt.Errorf("...: %w", err) 包装 os.ReadFile 的错误，
		// errors.Is 会沿着 wrap 链匹配 os.ErrNotExist。
		if errors.Is(err, os.ErrNotExist) {
			return doctorCheck{name: "凭证", passed: false, message: "未登录（运行 ks login）"}
		}
		return doctorCheck{name: "凭证", passed: false, message: fmt.Sprintf("凭证文件损坏: %v", err)}
	}
	email := cred.Email
	if email == "" {
		email = "(未记录邮箱)"
	}
	return doctorCheck{name: "凭证", passed: true, message: fmt.Sprintf("已登录: %s", email)}
}
