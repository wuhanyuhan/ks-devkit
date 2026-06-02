package auth

import "github.com/spf13/cobra"

// Cmd 是 ks auth 命令组的根命令
var Cmd = &cobra.Command{
	Use:   "auth",
	Short: "认证管理（注册、登录、登出、查看身份）",
}
