package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/tzone85/vortex-dispatch/internal/dashstart"
)

// ensureDashboardFunc is the seam used by runReq to launch / reuse the
// always-on dashboard daemon. Tests substitute a stub so they don't fork
// real processes.
var ensureDashboardFunc = dashstart.Ensure

// openBrowserFunc is the seam used by runReq to open the URL. Tests
// substitute a recorder.
var openBrowserFunc = dashstart.OpenBrowser

// printDashboardBanner takes care of the entire auto-spawn flow: it works
// out the daemon state dir, calls ensureDashboardFunc, prints the banner,
// and (unless headless) tries to open the browser.
//
// Failures are logged and swallowed. The hard rule from the spec is:
// dashboard auto-spawn MUST NOT block `vxd req` dispatch.
func printDashboardBanner(cmd *cobra.Command, s stores, reqID string) {
	out := cmd.OutOrStdout()

	self, err := os.Executable()
	if err != nil {
		log.Printf("[dashboard] cannot resolve self path: %v", err)
		return
	}

	stateDir := s.ProjectDir
	if stateDir == "" {
		// Fall back to ~/.vxd if the projection store didn't surface one.
		home, herr := os.UserHomeDir()
		if herr == nil && home != "" {
			stateDir = filepath.Join(home, ".vxd")
		} else {
			stateDir = filepath.Join(os.TempDir(), "vxd")
		}
	}

	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()

	port := s.Config.Dashboard.Port
	if port == 0 {
		port = 8787
	}

	handle, err := ensureDashboardFunc(ctx, dashstart.Config{
		Self:     self,
		StateDir: stateDir,
		Port:     port,
		NoOpen:   true, // the caller (this function) decides about the browser
	})
	if err != nil {
		log.Printf("[dashboard] auto-spawn failed: %v", err)
		fmt.Fprintf(out, "Dashboard: unavailable (%v) — continuing without it.\n", err)
		return
	}

	url := dashboardURLForReq(handle, reqID)

	verb := "Reusing"
	if !handle.Reused {
		verb = "Started"
	}
	fmt.Fprintf(out, "%s dashboard daemon (pid %d, port %d).\n", verb, handle.PID, handle.Port)
	fmt.Fprintf(out, "Dashboard: %s\n", url)

	stdoutIsTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	headless, reason := dashstart.IsHeadless(dashstart.OSEnv{}, stdoutIsTTY, s.Config.Dashboard.AutoOpen)
	if headless {
		fmt.Fprintf(out, "(browser not opened: %s)\n", reason)
		return
	}

	if err := openBrowserFunc(url); err != nil {
		log.Printf("[dashboard] open browser: %v", err)
	}
}

// dashboardURLForReq returns the URL the user's browser should land on so
// it (a) consumes the single-use bootstrap nonce on first request and (b)
// scopes the dashboard view to the freshly-submitted requirement.
func dashboardURLForReq(h dashstart.Handle, reqID string) string {
	if h.BootstrapNonce == "" {
		return fmt.Sprintf("%s/?req=%s", h.URL, reqID)
	}
	return fmt.Sprintf("%s/?req=%s&bootstrap=%s", h.URL, reqID, h.BootstrapNonce)
}
