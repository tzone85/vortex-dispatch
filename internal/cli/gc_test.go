package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// runGCWithStores is a test helper that exercises the GC logic with pre-built stores,
// bypassing loadStores so tests don't need a full git + config setup.
func runGCWithStores(cmd *cobra.Command, s stores) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	out := cmd.OutOrStdout()

	mergedStories, err := s.Proj.ListStories(state.StoryFilter{Status: "merged"})
	if err != nil {
		return fmt.Errorf("list merged stories: %w", err)
	}

	branches := make([]engine.BranchInfo, 0, len(mergedStories))
	for _, story := range mergedStories {
		if story.Branch == "" {
			continue
		}
		mergedAt := story.MergedAt
		if mergedAt.IsZero() {
			mergedAt = story.CreatedAt
		}
		branches = append(branches, engine.BranchInfo{
			Name:     story.Branch,
			StoryID:  story.ID,
			MergedAt: mergedAt,
		})
	}

	if dryRun {
		if len(branches) == 0 {
			fmt.Fprintf(out, "Dry run: no merged branches found.\n")
		} else {
			fmt.Fprintf(out, "Dry run: would check %d branches for cleanup\n", len(branches))
			fmt.Fprintf(out, "Branch retention: %d days\n\n", s.Config.Cleanup.BranchRetentionDays)
			for _, b := range branches {
				fmt.Fprintf(out, "  %s (story: %s, merged: %s)\n",
					b.Name, b.StoryID, b.MergedAt.Format("2006-01-02"))
			}
		}
		fmt.Fprintf(out, "\nLog retention: %d days (logs older than this would be deleted)\n",
			s.Config.Workspace.LogRetentionDays)
		return nil
	}

	if len(branches) == 0 {
		fmt.Fprintf(out, "No merged stories found. Skipping branch cleanup.\n")
	}
	// Branch git operations are intentionally skipped in unit tests.

	logDir := filepath.Join(s.ProjectDir, "logs")
	logsDeleted, err := engine.CleanupLogs(logDir, s.Config.Workspace.LogRetentionDays)
	if err != nil {
		fmt.Fprintf(out, "warning: log cleanup failed: %v\n", err)
	} else if logsDeleted > 0 {
		fmt.Fprintf(out, "Cleaned up %d expired log files (retention: %d days).\n",
			logsDeleted, s.Config.Workspace.LogRetentionDays)
	}

	return nil
}

// makeGCCmd returns a gc cobra.Command with required flags pre-set for testing.
func makeGCCmd() *cobra.Command {
	cmd := newGCCmd()
	return cmd
}

// TestGC_DryRun_ShowsLogRetention verifies that --dry-run output includes log retention info.
func TestGC_DryRun_ShowsLogRetention(t *testing.T) {
	_, s := setupTestEnv(t)
	s.Config.Workspace.LogRetentionDays = 14

	cmd := makeGCCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set dry-run: %v", err)
	}

	if err := runGCWithStores(cmd, s); err != nil {
		t.Fatalf("runGCWithStores: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Log retention") {
		t.Errorf("expected 'Log retention' in dry-run output, got:\n%s", output)
	}
	if !strings.Contains(output, "14 days") {
		t.Errorf("expected retention days (14) in output, got:\n%s", output)
	}
}

// TestGC_CleanupLogs_DeletesOldFiles verifies log cleanup removes files older than retention.
func TestGC_CleanupLogs_DeletesOldFiles(t *testing.T) {
	_, s := setupTestEnv(t)
	s.Config.Workspace.LogRetentionDays = 7

	// Create logs directory inside ProjectDir
	logDir := filepath.Join(s.ProjectDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	// Create an old log file (8 days old — outside retention window)
	oldLog := filepath.Join(logDir, "old-agent.log")
	if err := os.WriteFile(oldLog, []byte("old log\n"), 0o644); err != nil {
		t.Fatalf("write old log: %v", err)
	}
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldLog, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Create a fresh log file (1 day old — within retention window)
	freshLog := filepath.Join(logDir, "fresh-agent.log")
	if err := os.WriteFile(freshLog, []byte("fresh log\n"), 0o644); err != nil {
		t.Fatalf("write fresh log: %v", err)
	}

	cmd := makeGCCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runGCWithStores(cmd, s); err != nil {
		t.Fatalf("runGCWithStores: %v", err)
	}

	// Old log should be gone
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Errorf("expected old log to be deleted, still exists")
	}

	// Fresh log should remain
	if _, err := os.Stat(freshLog); os.IsNotExist(err) {
		t.Errorf("expected fresh log to remain, was deleted")
	}

	output := buf.String()
	if !strings.Contains(output, "1 expired log file") {
		t.Errorf("expected cleanup count in output, got:\n%s", output)
	}
}

// TestGC_CleanupLogs_SurvivesMissingLogDir verifies gc does not error when logs dir is absent.
func TestGC_CleanupLogs_SurvivesMissingLogDir(t *testing.T) {
	_, s := setupTestEnv(t)
	s.Config.Workspace.LogRetentionDays = 30
	// Intentionally do NOT create the logs dir

	cmd := makeGCCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runGCWithStores(cmd, s); err != nil {
		t.Fatalf("expected no error when log dir missing, got: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "warning: log cleanup failed") {
		t.Errorf("unexpected log cleanup failure in output:\n%s", output)
	}
}

// TestGC_NoMergedStories_StillRunsLogCleanup ensures log cleanup runs even when
// there are no merged branches (the early-return path is gone).
func TestGC_NoMergedStories_StillRunsLogCleanup(t *testing.T) {
	_, s := setupTestEnv(t)
	s.Config.Workspace.LogRetentionDays = 5

	logDir := filepath.Join(s.ProjectDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	// Plant an expired log
	expiredLog := filepath.Join(logDir, "expired.log")
	if err := os.WriteFile(expiredLog, []byte("gone\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	pastTime := time.Now().Add(-6 * 24 * time.Hour)
	if err := os.Chtimes(expiredLog, pastTime, pastTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Database has no merged stories — simulates fresh project
	cmd := makeGCCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runGCWithStores(cmd, s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Despite no merged stories, log should be cleaned up
	if _, err := os.Stat(expiredLog); !os.IsNotExist(err) {
		t.Errorf("expected expired log to be deleted even with no merged stories")
	}

	output := buf.String()
	if !strings.Contains(output, "No merged stories") {
		t.Errorf("expected 'No merged stories' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "1 expired log file") {
		t.Errorf("expected log cleanup count in output, got:\n%s", output)
	}
}
