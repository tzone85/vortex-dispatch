package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <req-id>",
		Short: "Print log file for a dispatched requirement",
		Long: `Print the log file captured when 'vxd req' self-daemonized.

The daemon redirects stdout+stderr to:
  ~/.vxd/projects/<project>/logs/req-<req-id>.log

Use -f to follow (tail -f style) — not supported in this MVP;
pipe through 'tail -f' manually if live following is needed.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         runLogs,
	}
	return cmd
}

func runLogs(cmd *cobra.Command, args []string) error {
	reqID := args[0]

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	logPath := reqLogPath(s.ProjectDir, reqID)

	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log file found for requirement %s at %s\n"+
				"(the req may have been run in foreground mode — check your terminal output)", reqID, logPath)
		}
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(cmd.OutOrStdout(), f); err != nil {
		return fmt.Errorf("read log file: %w", err)
	}
	return nil
}

// reqLogPath returns the path of the daemon log file for the given project dir
// and requirement ID.
func reqLogPath(projectDir, reqID string) string {
	return filepath.Join(projectDir, "logs", "req-"+reqID+".log")
}
