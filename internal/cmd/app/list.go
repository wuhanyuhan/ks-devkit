package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出我的应用",
	RunE:  runList,
}

func init() {
	Cmd.AddCommand(listCmd)
	listCmd.Flags().String("publisher", "", "按 publisher ID 过滤")
}

func runList(cmd *cobra.Command, args []string) error {
	cred, err := auth.LoadCredentials(auth.DefaultCredentialsPath())
	if err != nil {
		return fmt.Errorf("请先运行 ks auth login: %w", err)
	}

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	publisherID, _ := cmd.Flags().GetString("publisher")

	client := hub.NewClient(cfg.HubURL, cred.AccessToken)
	apps, err := client.ListApps(publisherID)
	if err != nil {
		return fmt.Errorf("获取应用列表失败: %w", err)
	}

	if len(apps) == 0 {
		fmt.Println("暂无应用，使用 ks app create 或 ks publish 创建")
		return nil
	}

	fmt.Printf("%-20s %-20s %-10s %s\n", "ID", "名称", "类型", "状态")
	for _, a := range apps {
		fmt.Printf("%-20s %-20s %-10s %s\n", a.AppID, a.Name, a.Type, a.Status)
	}
	return nil
}
