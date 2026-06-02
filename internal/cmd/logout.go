package cmd

import (
	"github.com/spf13/cobra"
	authcmd "github.com/wuhanyuhan/ks-devkit/internal/cmd/auth"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "登出 Keystone Hub（ks auth logout 的别名）",
	RunE:  authcmd.RunLogout,
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
