package cmd

import (
	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/buildinfo"
)

var rootCmd = &cobra.Command{
	Use:   "ks",
	Short: "Keystone 应用开发工具链",
	Long:  "ks 是 Keystone 开放生态平台的开发者 CLI，支持应用创建、构建、测试和发布。",
}

func Execute() error {
	return rootCmd.Execute()
}

// SetVersion 设置 CLI 版本号（由 main 包在编译时注入）
func SetVersion(v string) {
	buildinfo.SetVersion(v)
	rootCmd.Version = buildinfo.Version()
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "配置文件路径 (默认 ~/.ks/config.yaml)")
	rootCmd.PersistentFlags().String("hub-url", "", "ks-hub 服务地址")
	rootCmd.SilenceErrors = true // main.go 负责 stderr 打印
	rootCmd.SilenceUsage = true  // 业务错误不再附带 usage 帮助文本
}
