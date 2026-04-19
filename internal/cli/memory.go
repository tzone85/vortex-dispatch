package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/memory"
)

func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Launch the memory timeline dashboard",
		Long:  "Opens a browser-based dashboard for navigating VXD's institutional memory over time.\nShows findings, PRs, commits, and MemPalace search results on a timeline.",
		RunE:  runMemory,
	}
	cmd.Flags().Bool("web", false, "Launch web dashboard (required)")
	cmd.Flags().Bool("no-open", false, "Do not open a browser automatically")
	cmd.Flags().Int("port", 8078, "Web server port")
	cmd.SilenceUsage = true
	return cmd
}

func runMemory(cmd *cobra.Command, _ []string) error {
	isWeb, _ := cmd.Flags().GetBool("web")
	if !isWeb {
		return fmt.Errorf("the memory command requires --web flag. Run: vxd memory --web")
	}

	port, _ := cmd.Flags().GetInt("port")
	noOpen, _ := cmd.Flags().GetBool("no-open")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	auditDir := filepath.Join(cwd, "docs", "self-improvement")
	if _, err := os.Stat(auditDir); os.IsNotExist(err) {
		return fmt.Errorf("audit directory not found: %s\nRun this command from the VXD project root", auditDir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := memory.NewServer(auditDir, cwd, port)
	srv.SetOpenBrowserOnStart(!noOpen)
	if err := srv.Start(ctx); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("memory dashboard server: %w", err)
	}

	fmt.Println("Memory dashboard server stopped")
	return nil
}
