package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ProjectMetadata holds information about a VXD project.
type ProjectMetadata struct {
	Name         string    `json:"name"`
	RepoPath     string    `json:"repo_path"`
	RemoteURL    string    `json:"remote_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`

	// Migration-only fields
	MigratedFrom string `json:"migrated_from,omitempty"`
	MigratedAt   string `json:"migrated_at,omitempty"`
	Note         string `json:"note,omitempty"`
}

// nonAlphanumDash matches any character that is not a lowercase letter, digit, or dash.
var nonAlphanumDash = regexp.MustCompile(`[^a-z0-9-]`)

// multiDash collapses consecutive dashes into a single dash.
var multiDash = regexp.MustCompile(`-{2,}`)

// SanitizeProjectName normalises a raw name to a filesystem-safe project slug.
// Result is lowercase, alphanumeric + dashes, no leading/trailing dashes.
// The .git suffix is stripped. Returns "unnamed" for empty/all-symbol input.
func SanitizeProjectName(raw string) string {
	s := strings.ToLower(raw)
	s = strings.TrimSuffix(s, ".git")
	s = nonAlphanumDash.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "unnamed"
	}
	return s
}

// extractRepoName extracts the repository name from a git remote URL.
// Handles SSH (git@host:org/repo.git), HTTPS (https://host/org/repo.git),
// and nested paths (returns last segment only).
func extractRepoName(remoteURL string) string {
	// Take last path segment
	parts := strings.Split(remoteURL, "/")
	last := parts[len(parts)-1]
	// Handle SSH colon-separated paths like git@github.com:org/repo.git
	if colonIdx := strings.LastIndex(last, ":"); colonIdx >= 0 {
		last = last[colonIdx+1:]
		// If colon split gave us "org/repo.git", split again
		subParts := strings.Split(last, "/")
		last = subParts[len(subParts)-1]
	}
	return SanitizeProjectName(last)
}

// ResolveProjectName detects the project name from a git repository path.
// It first tries the origin remote URL, then falls back to the directory name
// of the repository root. Returns an error if the path is not inside a git repo.
func ResolveProjectName(repoPath string) (string, error) {
	// Verify this is a git repo
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = repoPath
	topOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", repoPath)
	}
	repoRoot := strings.TrimSpace(string(topOut))

	// Try origin remote URL
	cmd = exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoRoot
	remoteOut, err := cmd.Output()
	if err == nil {
		remoteURL := strings.TrimSpace(string(remoteOut))
		if remoteURL != "" {
			return extractRepoName(remoteURL), nil
		}
	}

	// Fallback: use directory name of repo root
	dirName := filepath.Base(repoRoot)
	return SanitizeProjectName(dirName), nil
}

// WriteMetadata writes project metadata to metadata.json in the given directory.
// Creates the directory if it does not exist.
func WriteMetadata(projectDir string, meta ProjectMetadata) error {
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	path := filepath.Join(projectDir, "metadata.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

// ReadMetadata reads project metadata from metadata.json in the given directory.
func ReadMetadata(projectDir string) (ProjectMetadata, error) {
	path := filepath.Join(projectDir, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectMetadata{}, fmt.Errorf("read metadata: %w", err)
	}

	var meta ProjectMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return ProjectMetadata{}, fmt.Errorf("parse metadata: %w", err)
	}
	return meta, nil
}

// ListProjects enumerates all projects under the VXD base directory.
// It reads metadata.json from each subdirectory of <baseDir>/projects/.
// Directories without a valid metadata.json are skipped.
func ListProjects(baseDir string) ([]ProjectMetadata, error) {
	projectsDir := filepath.Join(baseDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read projects dir: %w", err)
	}

	var projects []ProjectMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := ReadMetadata(filepath.Join(projectsDir, entry.Name()))
		if err != nil {
			// Skip directories without valid metadata
			continue
		}
		projects = append(projects, meta)
	}
	return projects, nil
}

// ProjectDir returns the full path to a project's state directory.
// baseDir is typically ~/.vxd.
func ProjectDir(baseDir, projectName string) string {
	return filepath.Join(baseDir, "projects", projectName)
}
