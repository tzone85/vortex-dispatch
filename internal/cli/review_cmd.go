package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review <story-id>",
		Short: "Review a story diff before approving",
		Args:  cobra.ExactArgs(1),
		RunE:  runReviewStory,
	}
	cmd.Flags().Bool("open", false, "Open PR in browser instead of showing diff")
	return cmd
}

func runReviewStory(cmd *cobra.Command, args []string) error {
	storyID := args[0]
	openBrowser, _ := cmd.Flags().GetBool("open")

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	story, err := s.Proj.GetStory(storyID)
	if err != nil {
		return fmt.Errorf("story %s not found", storyID)
	}

	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Story: %s\n", story.Title)
	fmt.Fprintf(out, "ID:     %s\n", story.ID)
	fmt.Fprintf(out, "Status: %s\n", story.Status)
	if story.PRNumber > 0 {
		fmt.Fprintf(out, "PR:     #%d \u2014 %s\n", story.PRNumber, story.PRUrl)
	}
	fmt.Fprintf(out, "Branch: %s\n", story.Branch)
	if story.AgentID != "" {
		fmt.Fprintf(out, "Agent:  %s\n", story.AgentID)
	}
	fmt.Fprintf(out, "Complexity: %d | Wave: %d | Escalation Tier: %d\n\n",
		story.Complexity, story.Wave, story.EscalationTier)

	if openBrowser && story.PRUrl != "" {
		openURL(story.PRUrl)
		fmt.Fprintln(out, "Opened PR in browser.")
		return nil
	}

	if story.Branch != "" {
		repoDir, _ := os.Getwd()
		diffStat, err := exec.Command("git", "-C", repoDir, "diff", "--stat",
			fmt.Sprintf("origin/%s...origin/%s", s.Config.Merge.BaseBranch, story.Branch)).Output()
		if err == nil && len(diffStat) > 0 {
			fmt.Fprintln(out, strings.Repeat("\u2500", 40)+" Diff "+strings.Repeat("\u2500", 40))
			fmt.Fprintln(out, strings.TrimSpace(string(diffStat)))
			fmt.Fprintln(out, strings.Repeat("\u2500", 85))
		}
	}

	fmt.Fprintln(out)
	if story.Status == "awaiting_approval" {
		fmt.Fprintf(out, "To approve: vxd approve %s\n", storyID)
		fmt.Fprintf(out, "To reject:  vxd reject %s \"feedback\"\n", storyID)
		if story.PRUrl != "" {
			fmt.Fprintf(out, "To open PR: vxd review %s --open\n", storyID)
		}
	}

	return nil
}

func openURL(url string) {
	// All branches: best-effort browser launch; if the helper isn't
	// installed (xdg-open missing on a headless Linux box) we just
	// continue, the URL is already printed for the user to copy.
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "windows":
		_ = exec.Command("cmd", "/c", "start", url).Start()
	}
}
