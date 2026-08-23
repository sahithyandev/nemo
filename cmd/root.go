package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed VERSION
var versionRaw string

var version = strings.TrimSpace(versionRaw)

var rootCmd = &cobra.Command{
	Use:   "nemo",
	Short: "nemo is a CLI tool for hiding files",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the nemo version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(versionCmd, newHideCommand(defaultHideDependencies()))
}
