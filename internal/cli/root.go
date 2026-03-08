package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Set via -ldflags at build time:
//
//	go build -ldflags "-X github.com/nullne/star-fleet/internal/cli.version=0.1.0"
var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "fleet",
	Short: "Star Fleet — autonomous PR delivery from GitHub issues",
	Long:  "A CLI tool that takes a GitHub Issue and autonomously delivers a human-ready PR.\nA single agent implements the feature and writes tests, then watches the PR for review feedback and CI results.",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("fleet " + version)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.Version = version
}

func Execute() error {
	return rootCmd.Execute()
}
