package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectBugFix(t *testing.T) {
	tests := []struct {
		title, desc string
		want        bool
	}{
		{"Fix null pointer in user handler", "", true},
		{"Bug: login fails with special characters", "", true},
		{"Investigate timeout in payment service", "", true},
		{"Debug race condition in cache", "", true},
		{"Add user authentication", "", false},
		{"Create REST API endpoints", "", false},
		{"Refactor database layer", "", false},
		{"", "The endpoint returns an error when called with empty payload", true},
		{"Update README", "Add deployment instructions", false},
		{"Patch security vulnerability in auth module", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := detectBugFix(tt.title, tt.desc)
			if got != tt.want {
				t.Errorf("detectBugFix(%q, %q) = %v, want %v", tt.title, tt.desc, got, tt.want)
			}
		})
	}
}

func TestDetectFrontend(t *testing.T) {
	tests := []struct {
		name        string
		title, desc string
		ownedFiles  []string
		want        bool
	}{
		{"ui-keyword-title", "Build the landing page", "", nil, true},
		{"component-keyword", "Create reusable Button component", "", nil, true},
		{"dashboard-keyword", "Admin dashboard with charts", "", nil, true},
		{"frontend-in-desc", "", "Implement the frontend for task management", nil, true},
		{"tailwind-keyword", "Style the app", "Use Tailwind for the layout", nil, true},
		{"react-keyword", "Task list view", "React component rendering tasks", nil, true},
		{"owned-tsx", "Wire task state", "", []string{"src/App.tsx"}, true},
		{"owned-css", "Polish spacing", "", []string{"styles/main.css"}, true},
		{"owned-vue", "Item editor", "", []string{"src/Editor.vue"}, true},
		{"owned-svelte", "Item editor", "", []string{"src/Editor.svelte"}, true},
		{"owned-html", "Static page", "", []string{"public/index.html"}, true},
		{"backend-only", "Create REST API endpoints", "Express routes for tasks", nil, false},
		{"db-story", "Add database migrations", "Postgres schema for users", nil, false},
		{"go-files-only", "Implement parser", "", []string{"internal/parser/parser.go"}, false},
		{"cli-story", "Add --json flag to CLI", "", []string{"cmd/root.go"}, false},
		// "server-side rendering of the page" mentions page — still frontend work.
		{"ssr", "Server-side rendering of the settings page", "", nil, true},
		// Substring traps: keywords must match whole words only.
		{"pagination-is-not-page", "Add pagination to the tasks API", "cursor-based pagination in the repository layer", nil, false},
		{"performance-is-not-form", "Improve performance of the query planner", "", nil, false},
		{"review-is-not-view", "Code review automation for PRs", "LLM review of diffs", nil, false},
		{"format-is-not-form", "Format output as JSON", "", nil, false},
		{"build-is-not-ui", "Build the release pipeline", "artifact signing", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectFrontend(tt.title, tt.desc, tt.ownedFiles)
			if got != tt.want {
				t.Errorf("detectFrontend(%q, %q, %v) = %v, want %v", tt.title, tt.desc, tt.ownedFiles, got, tt.want)
			}
		})
	}
}

func TestDetectInfrastructure(t *testing.T) {
	tests := []struct {
		title, desc string
		want        bool
	}{
		{"Fix Docker container startup", "", true},
		{"Update CI/CD pipeline", "", true},
		{"Configure nginx reverse proxy", "", true},
		{"Run database migration for new schema", "", true},
		{"Deploy to AWS", "", true},
		{"Add GitHub Actions workflow", "", true},
		{"Set up SSL certificate", "", true},
		{"Add user login form", "", false},
		{"Create REST API", "", false},
		{"Write unit tests", "", false},
		{"", "Update the environment variable for the database connection", true},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := detectInfrastructure(tt.title, tt.desc)
			if got != tt.want {
				t.Errorf("detectInfrastructure(%q, %q) = %v, want %v", tt.title, tt.desc, got, tt.want)
			}
		})
	}
}

func TestDetectExistingCodebase_NewRepo(t *testing.T) {
	dir := t.TempDir()
	// Init a brand new repo with 1 commit
	exec.Command("git", "init", dir).Run()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# New"), 0o644)
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	cmd.Run()

	if detectExistingCodebase(dir) {
		t.Error("brand new repo with 1 commit should NOT be detected as existing codebase")
	}
}

func TestDetectExistingCodebase_RealRepo(t *testing.T) {
	// Use the VXD repo itself — definitely an existing codebase
	cwd, _ := os.Getwd()
	// Walk up to find repo root (we might be in internal/engine/)
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			t.Skip("could not find repo root")
		}
		cwd = parent
	}

	if !detectExistingCodebase(cwd) {
		t.Error("VXD repo should be detected as existing codebase")
	}
}
