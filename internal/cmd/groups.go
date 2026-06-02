package cmd

import (
	appcmd "github.com/wuhanyuhan/ks-devkit/internal/cmd/app"
	authcmd "github.com/wuhanyuhan/ks-devkit/internal/cmd/auth"
	publishercmd "github.com/wuhanyuhan/ks-devkit/internal/cmd/publisher"
)

func init() {
	rootCmd.AddCommand(authcmd.Cmd)
	rootCmd.AddCommand(publishercmd.Cmd)
	rootCmd.AddCommand(appcmd.Cmd)
}
