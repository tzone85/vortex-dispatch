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

func init() {
	rootCmd.PersistentFlags().String("config", "vxd.yaml", "Path to config file")
	rootCmd.PersistentFlags().String("project", "", "Project name (auto-detected from git repo if not specified)")
	rootCmd.PersistentFlags().Bool("skip-preflight", false, "Skip pre-flight environment checks")

	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newReqCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newPauseCmd())
	rootCmd.AddCommand(newResumeCmd())
	rootCmd.AddCommand(newAgentsCmd())
	rootCmd.AddCommand(newEscalationsCmd())
	rootCmd.AddCommand(newGCCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newEventsCmd())
	rootCmd.AddCommand(newDashboardCmd())
	rootCmd.AddCommand(newArchiveCmd())
	rootCmd.AddCommand(newMemoryCmd())
	rootCmd.AddCommand(newOpportunityCmd())
	rootCmd.AddCommand(newMetricsCmd())
	rootCmd.AddCommand(newProjectsCmd())
	rootCmd.AddCommand(newDBCmd())
	rootCmd.AddCommand(newEstimateCmd())
	rootCmd.AddCommand(newPreflightCmd())
	rootCmd.AddCommand(newFigmaCmd())
	rootCmd.AddCommand(newReportCmd())
	rootCmd.AddCommand(newApprovePlanCmd())
	rootCmd.AddCommand(newRejectPlanCmd())
	rootCmd.AddCommand(newReviewCmd())
	rootCmd.AddCommand(newApproveCmd())
	rootCmd.AddCommand(newRejectCmd())
	rootCmd.AddCommand(newRetryCmd())
	rootCmd.AddCommand(newLearnCmd())
	rootCmd.AddCommand(newSecurityCmd())
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.AddCommand(newImproveCmd())
	rootCmd.AddCommand(newAutoresearchCmd())
	rootCmd.AddCommand(newLogsCmd())
	rootCmd.AddCommand(newWatchCmd())
	rootCmd.AddCommand(newReplayCmd())
}

func Execute() error {
	return rootCmd.Execute()
}
