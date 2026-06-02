package auth

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
	"github.com/wuhanyuhan/ks-devkit/internal/config"
	"github.com/wuhanyuhan/ks-devkit/internal/hub"
	"golang.org/x/term"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "注册 Keystone Hub 开发者账号",
	RunE:  runRegister,
}

func init() {
	Cmd.AddCommand(registerCmd)
	registerCmd.Flags().String("email", "", "邮箱地址")
	registerCmd.Flags().String("display-name", "", "显示名称")
}

func runRegister(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	email, _ := cmd.Flags().GetString("email")
	if email == "" {
		fmt.Print("邮箱: ")
		email, _ = reader.ReadString('\n')
		email = strings.TrimSpace(email)
	}

	displayName, _ := cmd.Flags().GetString("display-name")
	if displayName == "" {
		fmt.Print("显示名称: ")
		displayName, _ = reader.ReadString('\n')
		displayName = strings.TrimSpace(displayName)
	}

	fmt.Print("密码: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return fmt.Errorf("读取密码失败: %w", err)
	}
	fmt.Println()
	password := string(passwordBytes)

	client := hub.NewClient(cfg.HubURL, "")
	result, err := client.Register(email, password, displayName)
	if err != nil {
		return fmt.Errorf("注册失败: %w", err)
	}

	cred := &auth.Credentials{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Email:        email,
	}
	if err := auth.SaveCredentials(auth.DefaultCredentialsPath(), cred); err != nil {
		return fmt.Errorf("保存凭证失败: %w", err)
	}

	fmt.Printf("✓ 注册成功并已登录为 %s\n", email)
	return nil
}
