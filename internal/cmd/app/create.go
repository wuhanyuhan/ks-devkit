package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "创建新应用",
	RunE:  runCreate,
}

func init() {
	Cmd.AddCommand(createCmd)
	createCmd.Flags().String("publisher", "", "Publisher slug（必需）")
	createCmd.Flags().String("id", "", "应用 ID（必需）")
	createCmd.Flags().String("name", "", "应用名称（必需）")
	createCmd.Flags().String("type", "app", "应用类型 (app/squad/agent/skill)")
	createCmd.Flags().String("summary", "", "简介")
	_ = createCmd.MarkFlagRequired("publisher")
	_ = createCmd.MarkFlagRequired("id")
	_ = createCmd.MarkFlagRequired("name")
}

func runCreate(cmd *cobra.Command, args []string) error {
	cred, err := auth.LoadCredentials(auth.DefaultCredentialsPath())
	if err != nil {
		return fmt.Errorf("请先运行 ks auth login: %w", err)
	}

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	publisherSlug, _ := cmd.Flags().GetString("publisher")
	appID, _ := cmd.Flags().GetString("id")
	name, _ := cmd.Flags().GetString("name")
	appType, _ := cmd.Flags().GetString("type")
	summary, _ := cmd.Flags().GetString("summary")

	client := hub.NewClient(cfg.HubURL, cred.AccessToken)

	pub, err := client.GetPublisher(publisherSlug)
	if err != nil {
		return fmt.Errorf("查询 publisher %q 失败: %w", publisherSlug, err)
	}

	app, err := client.CreateApp(&hub.CreateAppRequest{
		PublisherID: pub.ID,
		AppID:       appID,
		Name:        name,
		Type:        appType,
		Summary:     summary,
	})
	if err != nil {
		return fmt.Errorf("创建应用失败: %w", err)
	}

	fmt.Printf("✓ 已创建应用: %s (%s)\n", app.AppID, app.Name)
	return nil
}
