package auth

import (
	"fmt"

	"github.com/spf13/cobra"
	localauth "github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "查看当前登录身份",
	RunE:  runWhoami,
}

func init() {
	Cmd.AddCommand(whoamiCmd)
}

func runWhoami(cmd *cobra.Command, args []string) error {
	cred, err := localauth.LoadCredentials(localauth.DefaultCredentialsPath())
	if err != nil {
		return fmt.Errorf("未登录，请先运行 ks auth login")
	}

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	client := hub.NewClient(cfg.HubURL, cred.AccessToken)
	profile, err := client.GetProfile()
	if err != nil {
		// 网络失败时回退到本地凭证信息
		fmt.Printf("邮箱: %s（离线模式，无法获取最新信息）\n", cred.Email)
		return nil
	}

	fmt.Printf("用户ID: %d\n", profile.UserID)
	fmt.Printf("邮箱:   %s\n", profile.Email)
	fmt.Printf("名称:   %s\n", profile.DisplayName)
	fmt.Printf("Hub:    %s\n", cfg.HubURL)
	return nil
}
