package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wuhanyuhan/ks-devkit/internal/build"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "构建应用包",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir := "."
		outputDir := filepath.Join(projectDir, "dist")

		result, err := build.Build(projectDir, outputDir)
		if err != nil {
			return err
		}

		fmt.Printf("应用: %s v%s\n", result.AppID, result.Version)
		fmt.Printf("包: %s\n", result.TarballPath)
		fmt.Printf("SHA256: %s\n", result.Checksum)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
}
