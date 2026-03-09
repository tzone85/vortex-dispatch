package cli

import (
	"github.com/spf13/cobra"
)

var version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:   "vxd",
	Short: "Vortex Dispatch -- AI agent orchestrator",
	Long:  "VXD orchestrates autonomous AI agents through the full software development lifecycle.\nHand off a requirement, walk away, come back to merged PRs.",
	Version: version,
}

func Execute() error {
	return rootCmd.Execute()
}
