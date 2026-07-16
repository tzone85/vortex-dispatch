package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/figma"
)

// figmaTokenSettingsURL is where a personal access token is created. Printed,
// never auto-opened — the operator clicks it themselves.
const figmaTokenSettingsURL = "https://www.figma.com/settings" // #nosec G101 -- public settings URL printed for the operator, not a credential

// figmaAPIBase overrides the Figma API base URL in tests ("" = production).
var figmaAPIBase = ""

// newFigmaClient builds a client honoring the test override.
func newFigmaClient(token string) *figma.Client {
	c := figma.NewClient(token)
	if figmaAPIBase != "" {
		c.BaseURL = figmaAPIBase
	}
	return c
}

func newFigmaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "figma",
		Short: "Figma design integration: authenticate and inspect design access",
		Long: `Requirements can reference figma.com design URLs; vxd pulls the referenced
frames (structure, styles, rendered PNGs) into a design context that the
planner and the frontend agents build against.

Figma access needs an operator credential, so the FIRST Figma-referencing run
is interactive-once rather than fire-and-forget: run 'vxd figma auth' a single
time, then Figma runs are autonomous again.`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newFigmaAuthCmd())
	cmd.AddCommand(newFigmaStatusCmd())
	return cmd
}

func newFigmaAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "One-time interactive session: store a Figma personal access token",
		Long: `Interactive (one time): create a personal access token in your Figma
settings, paste it here, and vxd validates + stores it at
<state_dir>/figma.token (mode 0600). Subsequent Figma-referencing runs are
fully autonomous.`,
		Args: cobra.NoArgs,
		RunE: runFigmaAuth,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runFigmaAuth(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	stateDir := expandHome(cfg.Workspace.StateDir)

	fmt.Fprintln(out, "Figma auth — one-time interactive session")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  1. Open your Figma settings (Security tab):")
	fmt.Fprintf(out, "     %s\n", figmaTokenSettingsURL)
	fmt.Fprintln(out, "  2. Under 'Personal access tokens', generate a token with File content: Read scope.")
	fmt.Fprintln(out, "  3. Paste it below (input is stored at "+figma.TokenPath(stateDir)+", mode 0600).")
	fmt.Fprintln(out, "")
	fmt.Fprint(out, "Token: ")

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("read token: %w", err)
	}
	token := strings.TrimSpace(line)
	if token == "" {
		return fmt.Errorf("no token entered")
	}

	// Validate before storing so a typo'd token fails HERE, in the
	// interactive session, not mid-pipeline hours later.
	me, err := newFigmaClient(token).Me(cmd.Context())
	if err != nil {
		return fmt.Errorf("token validation failed (not stored): %w", err)
	}

	path, err := figma.SaveToken(stateDir, token)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nAuthenticated as %s (%s). Token stored at %s.\n", me.Handle, me.Email, path)
	fmt.Fprintln(out, "Figma-referencing runs are now fire-and-forget like every other vxd run.")
	return nil
}

func newFigmaStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether Figma access is configured and for which account",
		Args:  cobra.NoArgs,
		RunE:  runFigmaStatus,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runFigmaStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	token, source, err := figma.ResolveToken(expandHome(cfg.Workspace.StateDir))
	if err != nil {
		fmt.Fprintln(out, "Figma: not configured")
		fmt.Fprintln(out, err.Error())
		return nil
	}
	me, err := newFigmaClient(token).Me(cmd.Context())
	if err != nil {
		fmt.Fprintf(out, "Figma: credential found (%s) but validation failed: %v\n", source, err)
		fmt.Fprintln(out, "Re-run `vxd figma auth` to refresh it.")
		return nil
	}
	fmt.Fprintf(out, "Figma: authenticated as %s (%s) via %s\n", me.Handle, me.Email, source)
	return nil
}
