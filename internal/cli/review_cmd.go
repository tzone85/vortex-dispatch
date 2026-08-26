package cli

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/criteria"
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

	if desc := strings.TrimSpace(story.Description); desc != "" {
		fmt.Fprintf(out, "Description:\n%s\n\n", desc)
	}
	if ac := criteria.FormatMarkdown(story.AcceptanceCriteria); ac != "" {
		fmt.Fprintf(out, "Acceptance Criteria:\n%s\n\n", ac)
	}

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

// cmdMetacharacters are active in cmd.exe's argument parser. A valid https URL
// can still contain them in its query/fragment (e.g.
// "https://github.com/x?a=1&calc.exe" parses cleanly yet `&` chains a second
// command under `cmd /c start`). url.Parse alone does NOT neutralize them.
const cmdMetacharacters = "&^|<>%\"`\n\r\t"

// safeBrowserURL validates rawURL for browser launch. A scheme/host check is
// not enough — a valid https URL can still carry cmd.exe metacharacters (see
// cmdMetacharacters). It returns the RE-SERIALIZED url.URL (never the raw
// input) and false if the URL is malformed or carries shell-active characters.
func safeBrowserURL(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	if strings.ContainsAny(rawURL, cmdMetacharacters) {
		return "", false
	}
	return u.String(), true
}

// startBrowser is the process-spawn seam for openURL. Tests override it so
// running the suite never opens real browser tabs on the host.
var startBrowser = func(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

func openURL(rawURL string) {
	safeURL, ok := safeBrowserURL(rawURL)
	if !ok {
		fmt.Printf("refusing to open unsafe or malformed URL: %q\n", rawURL)
		return
	}
	// All branches: best-effort browser launch; if the helper isn't
	// installed (xdg-open missing on a headless Linux box) we just
	// continue, the URL is already printed for the user to copy.
	switch runtime.GOOS {
	case "darwin":
		_ = startBrowser("open", safeURL)
	case "linux":
		_ = startBrowser("xdg-open", safeURL)
	case "windows":
		// rundll32 FileProtocolHandler opens the URL in the default browser
		// WITHOUT invoking cmd.exe's metacharacter parser, unlike `cmd /c start`.
		_ = startBrowser("rundll32", "url.dll,FileProtocolHandler", safeURL)
	}
}
