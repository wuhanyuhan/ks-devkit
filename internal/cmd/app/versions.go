package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var versionsCmd = &cobra.Command{
	Use:   "versions <app_id>",
	Short: "查看应用的版本列表",
	Args:  cobra.ExactArgs(1),
	RunE:  runVersions,
}

func init() {
	Cmd.AddCommand(versionsCmd)
}

func runVersions(cmd *cobra.Command, args []string) error {
	appID := args[0]

	cred, err := auth.LoadCredentials(auth.DefaultCredentialsPath())
	if err != nil {
		return fmt.Errorf("请先运行 ks auth login: %w", err)
	}

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	client := hub.NewClient(cfg.HubURL, cred.AccessToken)
	versions, err := client.ListVersions(appID)
	if err != nil {
		return fmt.Errorf("获取版本列表失败: %w", err)
	}

	if len(versions) == 0 {
		fmt.Printf("应用 %s 暂无版本，使用 ks publish 上传\n", appID)
		return nil
	}

	fmt.Printf("%-12s %-16s %s\n", "版本", "状态", "变更说明")
	for _, v := range versions {
		changelog := v.Changelog
		if len(changelog) > 40 {
			changelog = changelog[:40] + "..."
		}
		fmt.Printf("%-12s %-16s %s\n", v.Version, v.Status, changelog)
	}
	return nil
}
