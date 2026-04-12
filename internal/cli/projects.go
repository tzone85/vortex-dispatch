package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/repolearn"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"proj"},
		Short:   "List all VXD projects",
		Long:    "Shows all projects with their repo path, story count, and status.",
		RunE:    runProjects,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runProjects(cmd *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	vxdRoot := filepath.Join(home, ".vxd")
	projects, err := engine.ListProjects(vxdRoot)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	if len(projects) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No projects found. Run 'vxd init' in a git repo to create one.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tREPO PATH\tSTORIES\tMERGED\tLEARNED\tSTATUS")
	fmt.Fprintln(w, "-------\t---------\t-------\t------\t-------\t------")

	for _, p := range projects {
		stories, merged := countProjectStories(vxdRoot, p.Name)
		status := "active"
		if p.Name == "_legacy" {
			status = "migrated"
		}
		repoPath := p.RepoPath
		if p.Name == "_legacy" && repoPath == "" {
			repoPath = "(migrated from ~/.vxd)"
		}
		if len(repoPath) > 50 {
			repoPath = "..." + repoPath[len(repoPath)-47:]
		}
		learned := projectLearnStatus(vxdRoot, p.Name)
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\t%s\n", p.Name, repoPath, stories, merged, learned, status)
	}

	w.Flush()
	return nil
}

// projectLearnStatus returns a short summary of the learning state for a project.
func projectLearnStatus(vxdRoot, projectName string) string {
	projectDir := filepath.Join(vxdRoot, "projects", projectName)
	profile, err := repolearn.LoadProfile(projectDir)
	if err != nil || profile.TechStack.PrimaryLanguage == "" {
		return "none"
	}
	return fmt.Sprintf("iter %d (pass %s)", profile.Iteration, formatLearnPasses(profile.CompletedPasses))
}

func formatLearnPasses(passes []int) string {
	if len(passes) == 0 {
		return "-"
	}
	result := ""
	for i, p := range passes {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%d", p)
	}
	return result
}

func countProjectStories(vxdRoot, projectName string) (total, merged int) {
	dbPath := filepath.Join(vxdRoot, "projects", projectName, "vxd.db")
	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		return 0, 0
	}
	defer ps.Close()

	allStories, err := ps.ListStories(state.StoryFilter{})
	if err != nil {
		return 0, 0
	}

	total = len(allStories)
	for _, s := range allStories {
		if s.Status == "merged" || s.Status == "pr_submitted" {
			merged++
		}
	}
	return
}
