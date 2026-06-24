package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/dashboard"
	"github.com/tzone85/vortex-dispatch/internal/dashstart"
	"github.com/tzone85/vortex-dispatch/internal/state"
	"github.com/tzone85/vortex-dispatch/internal/web"
)

func newDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the live TUI dashboard",
		Long:  "Opens an interactive terminal dashboard showing story pipeline, agent status, event activity, and escalations. Use --web to launch a browser-based dashboard. Use --all to show requirements from all repos and archived ones.",
		RunE:  runDashboard,
	}
	cmd.Flags().Bool("all", false, "Show all requirements including archived and from other repos")
	cmd.Flags().Bool("web", false, "Launch web dashboard instead of TUI")
	cmd.Flags().Int("port", 8787, "Web server port")
	cmd.Flags().Bool("no-open", false, "(deprecated; no-open is now the default) Don't open browser automatically (web mode)")
	cmd.Flags().Bool("open", false, "Open the browser when the web dashboard starts (web mode). Off by default — the URL is always printed.")
	cmd.Flags().String("pidfile", "", "Write the web server's PID to this file (web mode only). Used by `vxd req` auto-spawn so later runs can reuse the daemon.")
	cmd.Flags().String("bootstrap-file", "", "Write the initial single-use bootstrap nonce to this file with mode 0o600 (web mode only). Used by `vxd req` auto-spawn.")
	cmd.SilenceUsage = true

	cmd.AddCommand(newDashboardStatusCmd())
	cmd.AddCommand(newDashboardStopCmd())
	return cmd
}

// newDashboardStatusCmd prints a one-line summary of the running dashboard
// daemon, or "not running" if none is detected. Pure read-only operation —
// no file is created or removed.
func newDashboardStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether the always-on dashboard daemon is running",
		Long:  "Reads ~/.vxd/dashboard.pid, probes the daemon's /health endpoint, and prints PID, port, uptime, and URL.",
		RunE:  runDashboardStatus,
	}
	cmd.Flags().String("pidfile", "", "Override the default pidfile path (~/.vxd/dashboard.pid)")
	cmd.Flags().Int("port", 8787, "Override the default dashboard port")
	cmd.SilenceUsage = true
	return cmd
}

// newDashboardStopCmd sends SIGTERM to the dashboard daemon and removes the
// pidfile. Idempotent: a stop on a non-running daemon prints a note and
// exits 0.
func newDashboardStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the always-on dashboard daemon",
		Long:  "Reads ~/.vxd/dashboard.pid, sends SIGTERM to the daemon, waits up to 5 seconds for shutdown, and removes the pidfile.",
		RunE:  runDashboardStop,
	}
	cmd.Flags().String("pidfile", "", "Override the default pidfile path (~/.vxd/dashboard.pid)")
	cmd.SilenceUsage = true
	return cmd
}

func runDashboard(cmd *cobra.Command, _ []string) error {
	showAll, _ := cmd.Flags().GetBool("all")

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	var filter state.ReqFilter
	if !showAll {
		cwd, _ := os.Getwd()
		filter.RepoPath = cwd
		filter.ExcludeArchived = true
	}

	isWeb, _ := cmd.Flags().GetBool("web")
	port, _ := cmd.Flags().GetInt("port")

	if isWeb {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		web.Version = version
		noOpen, _ := cmd.Flags().GetBool("no-open")
		openFlag, _ := cmd.Flags().GetBool("open")
		noOpen = dashboardNoOpen(noOpen, openFlag)
		pidfile, _ := cmd.Flags().GetString("pidfile")
		bootstrapFile, _ := cmd.Flags().GetString("bootstrap-file")

		srv := web.NewServer(s.Events, s.Proj, port, filter)
		srv.NoOpen = noOpen
		srv.Pidfile = pidfile
		srv.BootstrapFile = bootstrapFile
		if err := srv.Start(ctx); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("web server: %w", err)
		}
		fmt.Println("Dashboard server stopped")
		return nil
	}

	model := dashboard.New(s.Events, s.Proj, version, filter)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("dashboard error: %w", err)
	}

	return nil
}

// defaultDashboardPidfile returns the canonical pidfile path used by both
// auto-spawn (`vxd req`) and the status/stop subcommands. Kept here so the
// CLI side of the world has a single source of truth that mirrors the
// dashstart package's PidfilePath helper.
func defaultDashboardPidfile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "vxd-dashboard.pid")
	}
	return filepath.Join(home, ".vxd", "dashboard.pid")
}

func runDashboardStatus(cmd *cobra.Command, _ []string) error {
	pidfile, _ := cmd.Flags().GetString("pidfile")
	if pidfile == "" {
		pidfile = defaultDashboardPidfile()
	}
	port, _ := cmd.Flags().GetInt("port")

	out := cmd.OutOrStdout()

	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
	defer cancel()

	info, err := dashstart.IsAlive(ctx, &http.Client{Timeout: 1500 * time.Millisecond}, pidfile, port)
	if err != nil {
		fmt.Fprintln(out, "Dashboard: not running")
		fmt.Fprintf(out, "Pidfile:   %s\n", pidfile)
		return nil
	}

	uptime := ""
	if pidStartTime, err := pidStarted(info.PID); err == nil && !pidStartTime.IsZero() {
		uptime = fmt.Sprintf(" (up %s)", time.Since(pidStartTime).Truncate(time.Second))
	}

	fmt.Fprintf(out, "Dashboard: running%s\n", uptime)
	fmt.Fprintf(out, "PID:       %d\n", info.PID)
	fmt.Fprintf(out, "Port:      %d\n", info.Port)
	fmt.Fprintf(out, "URL:       http://localhost:%d\n", info.Port)
	fmt.Fprintf(out, "Pidfile:   %s\n", pidfile)
	return nil
}

func runDashboardStop(cmd *cobra.Command, _ []string) error {
	pidfile, _ := cmd.Flags().GetString("pidfile")
	if pidfile == "" {
		pidfile = defaultDashboardPidfile()
	}

	out := cmd.OutOrStdout()

	data, err := os.ReadFile(pidfile)
	if err != nil {
		fmt.Fprintln(out, "Dashboard: not running (no pidfile)")
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("malformed pidfile %s: %w", pidfile, err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(out, "Dashboard: pid %d not found\n", pid)
		_ = os.Remove(pidfile)
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// On Unix, ESRCH means the process is already gone.
		fmt.Fprintf(out, "Dashboard: pid %d already gone\n", pid)
		_ = os.Remove(pidfile)
		return nil
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, alive := dashstart.IsAlive(cmd.Context(), &http.Client{Timeout: 200 * time.Millisecond}, pidfile, 8787); alive != nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	_ = os.Remove(pidfile)
	fmt.Fprintf(out, "Dashboard stopped (pid %d).\n", pid)
	return nil
}

// pidStarted returns the start time of pid. Best-effort: returns zero time
// without an error when /proc isn't available (macOS / Windows / containerised
// without /proc). The status command treats a zero return as "uptime
// unavailable" and just elides that line — never fails because of it.
func pidStarted(pid int) (time.Time, error) {
	statPath := fmt.Sprintf("/proc/%d", pid)
	st, err := os.Stat(statPath)
	if err != nil {
		return time.Time{}, nil //nolint:nilerr // intentional: no /proc → no uptime
	}
	return st.ModTime(), nil
}

// dashboardNoOpen decides whether the web dashboard daemon should SKIP opening a
// browser. No-open is the default: a browser opens only when --open is passed
// explicitly, or never when --no-open is passed. This stops auto-spawned (or any
// stray) daemons from popping browser windows — the URL is always printed.
func dashboardNoOpen(noOpenFlag, openFlag bool) bool {
	return noOpenFlag || !openFlag
}
