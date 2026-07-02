package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// detectExistingCodebase checks if the repo already has meaningful source code.
// Returns true if the repo has more than just a scaffold — it has existing files,
// git history, and potentially tests. This triggers the CodebaseArchaeology and
// LegacyCodeSurvival diagnostic playbooks.
func detectExistingCodebase(repoPath string) bool {
	// Check for git history beyond the initial commit
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	commitCount := strings.TrimSpace(string(out))
	// More than 3 commits = existing codebase with history
	if commitCount != "" && commitCount != "0" && commitCount != "1" && commitCount != "2" && commitCount != "3" {
		return true
	}

	// Check for existing source files beyond scaffolding
	sourceCount := 0
	extensions := []string{".go", ".py", ".ts", ".js", ".rs", ".java", ".rb", ".php", ".swift", ".kt"}
	//nolint:errcheck // best-effort source detector; callback handles per-path errors
	filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			// Skip hidden dirs and vendor/node_modules
			if info != nil && info.IsDir() {
				name := info.Name()
				if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		for _, ext := range extensions {
			if strings.HasSuffix(info.Name(), ext) {
				sourceCount++
				if sourceCount >= 10 {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	return sourceCount >= 10
}

// detectBugFix checks if the story title or description indicates a bug fix.
// This triggers the BugHuntingMethodology diagnostic playbook.
func detectBugFix(title, description string) bool {
	lower := strings.ToLower(title + " " + description)
	bugKeywords := []string{
		"fix", "bug", "broken", "crash", "error", "fail",
		"regression", "issue", "defect", "patch", "hotfix",
		"not working", "doesn't work", "doesn't work",
		"incorrect", "wrong", "unexpected",
		"null pointer", "nil pointer", "panic",
		"timeout", "hang", "stuck", "deadlock",
		"memory leak", "race condition",
		"debug", "diagnose", "troubleshoot", "investigate",
	}
	for _, kw := range bugKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// frontendFileExts are file extensions that mark a story as UI-facing when they
// appear in its owned files.
var frontendFileExts = map[string]bool{
	".tsx": true, ".jsx": true, ".vue": true, ".svelte": true,
	".css": true, ".scss": true, ".sass": true, ".less": true,
	".html": true, ".astro": true,
}

// detectFrontend checks if the story builds or changes a user-facing web UI.
// This triggers the FrontendDesignBrief injection (agent.FrontendDesignBrief)
// so agents produce distinctive, accessible frontends instead of generic
// AI-default design. Detection combines owned-file extensions (strongest
// signal) with title/description keywords.
func detectFrontend(title, description string, ownedFiles []string) bool {
	for _, f := range ownedFiles {
		if frontendFileExts[strings.ToLower(filepath.Ext(f))] {
			return true
		}
	}
	return frontendKeywordRe.MatchString(strings.ToLower(title + " " + description))
}

// frontendKeywordRe matches UI vocabulary as whole words only — plain substring
// matching trips on "pagination" (page), "performance" (form), "review" (view).
// Deliberately absent: "html" (server-side HTML emails/reports are backend
// work; real UI stories carry .html in owned files or another keyword) and
// "responsive" ("responsive API gateway" means fast, not responsive design).
// Hoisted to package level so detection never recompiles it (perf convention).
var frontendKeywordRe = regexp.MustCompile(`\b(` +
	`frontend|front-end|ui|ux|user interface|` +
	`landing page|page|screen|view|component|widget|` +
	`dashboard|layout|styling|stylesheet|css|` +
	`tailwind|react|vue|svelte|next\.js|nextjs|astro|` +
	`design system|web app|webapp|website|` +
	`form|modal|navbar|navigation bar|sidebar|button|` +
	`theme|dark mode|typography` +
	`)\b`)

// detectInfrastructure checks if the story involves Docker, CI/CD, deployment,
// or infrastructure concerns. This triggers the InfrastructureDebugging playbook.
func detectInfrastructure(title, description string) bool {
	lower := strings.ToLower(title + " " + description)
	infraKeywords := []string{
		"docker", "container", "kubernetes", "k8s",
		"deploy", "deployment", "ci/cd", "pipeline",
		"github actions", "workflow", "ci.yml",
		"nginx", "apache", "reverse proxy", "load balancer",
		"database migration", "schema migration",
		"ssl", "tls", "certificate", "https",
		"dns", "domain", "port", "firewall",
		"environment variable", "env var", ".env",
		"server", "hosting", "cloud", "aws", "gcp", "azure",
		"monitoring", "logging", "alerting",
		"terraform", "ansible", "helm",
		"systemd", "service", "daemon",
	}
	for _, kw := range infraKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
