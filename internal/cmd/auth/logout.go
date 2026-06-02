package auth

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/auth"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "登出 Keystone Hub",
	RunE:  RunLogout,
}

func init() {
	Cmd.AddCommand(logoutCmd)
}

// RunLogout 执行登出逻辑，同时被 ks logout 别名使用
func RunLogout(cmd *cobra.Command, args []string) error {
	if err := auth.DeleteCredentials(auth.DefaultCredentialsPath()); err != nil {
		return fmt.Errorf("登出失败: %w", err)
	}
	fmt.Println("✓ 已登出")
	return nil
}
