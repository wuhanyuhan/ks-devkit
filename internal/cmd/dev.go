package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/resources"
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "启动本地开发环境",
	Long:  "通过 docker compose 拉起 Keystone + MySQL + Redis，供本地调试 MCP Service。",
	RunE:  runDev,
}

func init() {
	rootCmd.AddCommand(devCmd)
}

func runDev(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("需要安装 Docker: %w", err)
	}

	composeFile, err := resolveComposeFile()
	if err != nil {
		return err
	}

	fmt.Println("启动本地开发环境...")
	c := exec.Command("docker", "compose", "-f", composeFile, "up", "-d")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ 开发环境已启动")
	fmt.Println("  Keystone API:  http://localhost:9988")
	fmt.Println("  MySQL:         localhost:3306")
	fmt.Println("  Redis:         localhost:6379")
	fmt.Println()
	fmt.Println("  管理后台账号:  admin / admin（首次登录需改密）")
	fmt.Println("  API Key:       ks_deadbeefdeadbeefdeadbeefdeadbeef")
	fmt.Println()
	fmt.Println("  测试命令:")
	fmt.Println(`    curl -X POST http://localhost:9988/v1/open/chat/completions \`)
	fmt.Println(`      -H "X-API-Key: ks_deadbeefdeadbeefdeadbeefdeadbeef" \`)
	fmt.Println(`      -d '{"agent_id":9001,"messages":[{"role":"user","content":"你好"}]}'`)
	return nil
}

// resolveComposeFile 按优先级查找 docker-compose.yaml：
// 1. 当前目录或 runtime/ 下（开发 ks-devkit 本身时）
// 2. ~/.ks/runtime/（从内嵌资源释放）
func resolveComposeFile() (string, error) {
	if f := findLocalComposeFile(); f != "" {
		return f, nil
	}
	return ensureEmbeddedRuntime()
}

func findLocalComposeFile() string {
	candidates := []string{
		"docker-compose.yaml",
		"docker-compose.yml",
		"runtime/docker-compose.yaml",
		"runtime/docker-compose.yml",
	}
	for _, name := range candidates {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}
	return ""
}

// ensureEmbeddedRuntime 确保 ~/.ks/runtime/ 存在且版本匹配。
func ensureEmbeddedRuntime() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户目录: %w", err)
	}

	runtimeDir := filepath.Join(home, ".ks", "runtime")
	composeFile := filepath.Join(runtimeDir, "docker-compose.yaml")
	versionFile := filepath.Join(runtimeDir, ".version")

	// 版本匹配时直接复用
	if data, err := os.ReadFile(versionFile); err == nil {
		if string(data) == rootCmd.Version {
			return composeFile, nil
		}
		fmt.Printf("检测到版本更新 (%s → %s)，重新释放运行时文件...\n", string(data), rootCmd.Version)
	}

	// 从内嵌 FS 释放
	fmt.Println("释放运行时文件到 ~/.ks/runtime/ ...")
	if err := extractEmbeddedRuntime(runtimeDir); err != nil {
		return "", fmt.Errorf("释放运行时文件失败: %w", err)
	}

	// 写入版本标记
	if err := os.WriteFile(versionFile, []byte(rootCmd.Version), 0644); err != nil {
		return "", fmt.Errorf("写入版本标记失败: %w", err)
	}

	return composeFile, nil
}

// extractEmbeddedRuntime 将内嵌的 runtime/ 目录内容释放到 targetDir。
func extractEmbeddedRuntime(targetDir string) error {
	return fs.WalkDir(resources.RuntimeFS, "runtime", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel("runtime", path)
		target := filepath.Join(targetDir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		content, err := fs.ReadFile(resources.RuntimeFS, path)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, content, 0644)
	})
}
