package cmd

import (
	"github.com/spf13/cobra"
	authcmd "github.com/wuhanyuhan/ks-devkit/internal/cmd/auth"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "登录 Keystone Hub（ks auth login 的别名）",
	RunE:  authcmd.RunLogin,
}

func init() {
	rootCmd.AddCommand(loginCmd)
	authcmd.AddLoginFlags(loginCmd)
}
