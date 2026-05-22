package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/preflight"
)

func newPreflightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check environment readiness before dispatching",
		Long:  "Validates that all required tools, credentials, and configuration are present.\nRun before dispatching agents to catch issues early.",
		RunE:  runPreflight,
	}
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}

func runPreflight(cmd *cobra.Command, _ []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, _ := loadConfig(cfgPath) // best-effort; falls back to defaults on error

	report := preflight.RunAll(preflight.AllChecksWithConfig(cfg))

	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		preflight.FormatVerbose(cmd.OutOrStdout(), report)
	}

	if report.HasCritical {
		os.Exit(1)
	}
	return nil
}

// runDispatchPreflight is the shared preflight block for vxd req and vxd resume.
// Returns an error if critical checks fail, nil otherwise.
func runDispatchPreflight(cmd *cobra.Command) error {
	skip, _ := cmd.Flags().GetBool("skip-preflight")
	if skip {
		return nil
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, _ := loadConfig(cfgPath) // best-effort; falls back to defaults on error

	report := preflight.RunAll(preflight.DispatchChecksWithConfig(cfg))
	if report.HasCritical {
		preflight.FormatCompact(cmd.ErrOrStderr(), report)
		return fmt.Errorf("aborting: critical pre-flight issues")
	}
	if report.HasWarning {
		preflight.FormatCompact(cmd.ErrOrStderr(), report)
	}
	return nil
}
