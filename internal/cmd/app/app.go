package app

import "github.com/spf13/cobra"

// Cmd 是 ks app 命令组的根命令
var Cmd = &cobra.Command{
	Use:   "app",
	Short: "应用管理（创建、列表、版本、提审）",
}
