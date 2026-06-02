package publisher

import "github.com/spf13/cobra"

// Cmd 是 ks publisher 命令组的根命令
var Cmd = &cobra.Command{
	Use:   "publisher",
	Short: "Publisher 管理（创建、列表）",
}
