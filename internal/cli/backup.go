package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
)

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup archive of the project state",
		Long: `Creates a tar.gz archive of the project state directory containing
events.jsonl, store.db, and configuration files.

Example:
  vxd backup                    # backup to current directory
  vxd backup --output /backups  # backup to specific directory`,
		RunE: runBackup,
	}
	cmd.Flags().StringP("output", "o", ".", "Output directory for the backup archive")
	cmd.SilenceUsage = true
	return cmd
}

func runBackup(cmd *cobra.Command, _ []string) error {
	outputDir, _ := cmd.Flags().GetString("output")

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	archivePath, err := engine.CreateBackup(s.ProjectDir, outputDir)
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Backup created: %s\n", archivePath)
	return nil
}
