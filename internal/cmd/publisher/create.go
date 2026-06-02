package publisher

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "创建新的 Publisher",
	RunE:  runCreate,
}

func init() {
	Cmd.AddCommand(createCmd)
	createCmd.Flags().String("slug", "", "Publisher 唯一标识（必需）")
	createCmd.Flags().String("name", "", "显示名称（必需）")
	_ = createCmd.MarkFlagRequired("slug")
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

	slug, _ := cmd.Flags().GetString("slug")
	name, _ := cmd.Flags().GetString("name")

	client := hub.NewClient(cfg.HubURL, cred.AccessToken)
	pub, err := client.CreatePublisher(slug, name)
	if err != nil {
		return fmt.Errorf("创建 publisher 失败: %w", err)
	}

	fmt.Printf("✓ 已创建 publisher: %s (id=%d)\n", pub.Slug, pub.ID)
	return nil
}
