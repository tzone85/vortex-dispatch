package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newGCCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Run garbage collection on branches and worktrees",
		Long:  "Removes merged branches that have exceeded the retention period. Use --dry-run to preview without deleting.",
		RunE:  runGC,
	}
	cmd.Flags().Bool("dry-run", false, "Preview what would be cleaned up without deleting")
	cmd.SilenceUsage = true
	return cmd
}

func runGC(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	out := cmd.OutOrStdout()

	// -------------------------------------------------------------------------
	// Branch cleanup
	// -------------------------------------------------------------------------

	// Find merged stories to build branch info
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
			mergedAt = story.CreatedAt // fallback for legacy data
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
	} else {
		gitOps := &cliGitCleanupOps{}
		reaper := engine.NewReaper(s.Config.Cleanup, gitOps, s.Events)

		repoDir := "."
		deleted, err := reaper.GarbageCollect(repoDir, branches)
		if err != nil {
			return fmt.Errorf("garbage collect: %w", err)
		}

		if deleted == 0 {
			fmt.Fprintf(out, "No branches eligible for cleanup.\n")
		} else {
			fmt.Fprintf(out, "Cleaned up %d branches.\n", deleted)
		}
	}

	// -------------------------------------------------------------------------
	// Log cleanup — runs regardless of whether any branches were found
	// -------------------------------------------------------------------------

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

// cliGitCleanupOps implements engine.GitCleanupOps using real git commands.
type cliGitCleanupOps struct{}

func (g *cliGitCleanupOps) DeleteWorktree(repoDir, worktreePath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = repoDir
	return cmd.Run()
}

func (g *cliGitCleanupOps) DeleteBranch(repoDir, branch string) error {
	cmd := exec.Command("git", "branch", "-D", branch)
	cmd.Dir = repoDir
	return cmd.Run()
}

func (g *cliGitCleanupOps) BranchExists(repoDir, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", branch)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}
