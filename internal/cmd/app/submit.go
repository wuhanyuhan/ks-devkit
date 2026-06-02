package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var submitCmd = &cobra.Command{
	Use:   "submit <app_id> <version>",
	Short: "提交版本审核",
	Args:  cobra.ExactArgs(2),
	RunE:  runSubmit,
}

func init() {
	Cmd.AddCommand(submitCmd)
}

func runSubmit(cmd *cobra.Command, args []string) error {
	appID := args[0]
	version := args[1]

	cred, err := auth.LoadCredentials(auth.DefaultCredentialsPath())
	if err != nil {
		return fmt.Errorf("请先运行 ks auth login: %w", err)
	}

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	client := hub.NewClient(cfg.HubURL, cred.AccessToken)
	if _, err := client.SubmitVersion(appID, version); err != nil {
		return fmt.Errorf("提交审核失败: %w", err)
	}

	fmt.Printf("✓ 已提交 %s v%s 审核\n", appID, version)
	return nil
}
