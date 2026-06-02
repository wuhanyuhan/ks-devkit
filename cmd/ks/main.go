package main

import (
	"fmt"
	"os"

	"github.com/wuhanyuhan/ks-devkit/internal/cmd"
	"github.com/wuhanyuhan/ks-devkit/internal/cmd/exitcode"
)

var version = "dev"

func main() {
	cmd.SetVersion(version)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitcode.Extract(err))
	}
}
