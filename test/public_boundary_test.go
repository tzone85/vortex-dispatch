package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Keep rules generic: listing private product names here would disclose them.
func publicBoundaryViolation(path, content string) string {
	for _, prefix := range []string{".claude/", "docs/opportunities/", "docs/self-improvement/", "docs/audits/"} {
		if strings.HasPrefix(path, prefix) {
			return "operator artifacts must remain outside the published tree"
		}
	}
	name := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(name, ".md") && strings.Contains(name, "crossport") {
		return "cross-product porting plans are not public contributor documentation"
	}
	for _, privateDesign := range []string{"commercialization-strategy", "revenue-engine", "self-improvement-engine"} {
		if strings.HasSuffix(name, ".md") && strings.Contains(name, privateDesign) {
			return "operator strategy and plans must remain outside the published tree"
		}
	}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		return ""
	}
	for _, rule := range publicationRules {
		if rule.pattern.MatchString(content) {
			return rule.reason
		}
	}
	return ""
}

var publicationRules = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)/(users|home)/[a-z0-9_.-]+/`), "use a placeholder instead of a personal home directory"},
	{regexp.MustCompile(`(?im)^#{1,6}\s+.*(revenue projections|competitive positioning|commercialization strategy|deployment strategy)`), "internal business strategy does not belong in public documentation"},
	{regexp.MustCompile(`(?i)\*\*revenue target:\*\*`), "internal revenue targets do not belong in public documentation"},
}

func TestPublicBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		path, text string
		blocked    bool
	}{
		{".claude/plans/draft.md", "plan", true},
		{"docs/opportunities/customer.json", "{}", true},
		{"OTHER_CROSSPORT_REQUIREMENTS.md", "plan", true},
		{"REQUIREMENT_OTHER_CROSSPORT.md", "plan", true},
		{"docs/specs/2026-01-01-commercialization-strategy.md", "plan", true},
		{"docs/guide.md", "cd /Users/operator/project", true},
		{"docs/guide.md", "cd /home/operator/project", true},
		{"docs/guide.md", "## 10. Revenue Projections\nforecast", true},
		{"docs/guide.md", "**Revenue Target:** $100", true},
		{"docs/guide.md", "cd /Users/<you>/project", false},
		{"docs/guide.md", "## Deployment\nRun the public CLI.", false},
		{"docs/OPEN_CORE.md", "Keep commercial strategy and revenue targets private.", false},
	} {
		t.Run(tc.path+tc.text, func(t *testing.T) {
			if got := publicBoundaryViolation(tc.path, tc.text) != ""; got != tc.blocked {
				t.Fatalf("blocked=%v, want %v", got, tc.blocked)
			}
		})
	}
}

// Git's inventory includes artifacts already tracked despite .gitignore.
func TestPublicBoundary(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("enumerate publication inventory: %v", err)
	}
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if os.IsNotExist(err) { // Allow deletions in the patch under review.
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if reason := publicBoundaryViolation(path, string(data)); reason != "" {
			t.Errorf("%s: %s", path, reason)
		}
	}
}
