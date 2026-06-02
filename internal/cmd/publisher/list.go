package publisher

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出我的 Publisher",
	RunE:  runList,
}

func init() {
	Cmd.AddCommand(listCmd)
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

	client := hub.NewClient(cfg.HubURL, cred.AccessToken)
	pubs, err := client.ListPublishers()
	if err != nil {
		return fmt.Errorf("获取 publisher 列表失败: %w", err)
	}

	if len(pubs) == 0 {
		fmt.Println("暂无 publisher，使用 ks publisher create 创建")
		return nil
	}

	fmt.Printf("%-6s %-20s %s\n", "ID", "Slug", "名称")
	for _, p := range pubs {
		fmt.Printf("%-6d %-20s %s\n", p.ID, p.Slug, p.DisplayName)
	}
	return nil
}
