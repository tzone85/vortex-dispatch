package engine

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hallucination patterns that LLMs may prefix to generated files.
// These are reasoning/commentary lines that are NOT valid code.
var hallucinationPrefixes = []string{
	"Looking at ",
	"Let me ",
	"I'll ",
	"I've ",
	"I can see ",
	"I need to ",
	"I will ",
	"Here's ",
	"Here is ",
	"Based on ",
	"Now I ",
	"Now let me ",
	"The following ",
	"This is the ",
	"**Key ",
	"**Resolution ",
	"## ",
	"# Resolution",
	"Sure,",
	"Sure!",
	"Absolutely",
	"Great,",
	"OK,",
	"Okay,",
}

// scrubHallucinationsFromWorktree scans all tracked source files in the
// worktree for LLM hallucination preamble (reasoning text that leaked into
// the committed code). If found, the preamble lines are stripped and the
// file is re-written. Returns the number of files cleaned.
func scrubHallucinationsFromWorktree(worktreePath string) int {
	// Get list of tracked files
	cmd := exec.Command("git", "diff", "--name-only", "HEAD~1")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: scan all staged files
		cmd = exec.Command("git", "diff", "--cached", "--name-only")
		cmd.Dir = worktreePath
		out, _ = cmd.CombinedOutput()
	}

	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	cleaned := 0

	for _, relPath := range files {
		if relPath == "" {
			continue
		}

		// Only check source code files
		ext := strings.ToLower(filepath.Ext(relPath))
		if !isSourceExt(ext) {
			continue
		}

		absPath := filepath.Join(worktreePath, relPath)
		if scrubFile(absPath) {
			log.Printf("[sanitize] stripped hallucination preamble from %s", relPath)
			cleaned++
		}
	}

	if cleaned > 0 {
		// Re-stage and amend the commit
		addCmd := exec.Command("git", "add", "-A")
		addCmd.Dir = worktreePath
		addCmd.Run()

		commitCmd := exec.Command("git", "commit", "--amend", "--no-edit")
		commitCmd.Dir = worktreePath
		commitCmd.Run()

		log.Printf("[sanitize] amended commit: cleaned %d file(s) with hallucination preamble", cleaned)
	}

	return cleaned
}

// scrubFile checks a single file for hallucination preamble at the top.
// Returns true if the file was modified.
func scrubFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return false
	}

	// Find how many leading lines are hallucination
	cutAt := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// Empty lines between hallucination lines are also cut
			if i > 0 && cutAt == i {
				cutAt = i + 1
				continue
			}
			break
		}
		if isHallucinationLine(trimmed) {
			cutAt = i + 1
		} else {
			break
		}
	}

	if cutAt == 0 {
		return false
	}

	// Don't strip if it would remove all content
	remaining := lines[cutAt:]
	if len(strings.TrimSpace(strings.Join(remaining, "\n"))) == 0 {
		log.Printf("[sanitize] WARNING: %s is entirely hallucination — skipping (needs manual fix)", path)
		return false
	}

	// Write back without the preamble
	cleaned := strings.Join(remaining, "\n")
	if err := os.WriteFile(path, []byte(cleaned), 0644); err != nil {
		log.Printf("[sanitize] failed to write cleaned file %s: %v", path, err)
		return false
	}
	return true
}

// isHallucinationLine checks if a line matches known LLM preamble patterns.
func isHallucinationLine(line string) bool {
	for _, prefix := range hallucinationPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	// Also catch numbered lists that are part of reasoning (e.g., "1. **Combined Imports**:")
	if len(line) > 2 && line[0] >= '1' && line[0] <= '9' && line[1] == '.' && line[2] == ' ' {
		if strings.Contains(line, "**") {
			return true
		}
	}
	return false
}

// isSourceExt returns true for file extensions that should be checked for hallucination.
func isSourceExt(ext string) bool {
	switch ext {
	case ".js", ".ts", ".tsx", ".jsx", ".py", ".go", ".rs", ".java", ".rb",
		".vue", ".svelte", ".css", ".scss", ".html", ".json", ".yaml", ".yml",
		".sh", ".bash", ".zsh", ".fish", ".sql", ".graphql", ".proto":
		return true
	}
	return false
}

// validateBuild runs a build command in the worktree to verify the agent's
// output is syntactically valid. Returns an error if the build fails.
func validateBuild(worktreePath string) error {
	// Detect project type and run appropriate validation
	if fileExists(filepath.Join(worktreePath, "package.json")) {
		return validateNodeProject(worktreePath)
	}
	if fileExists(filepath.Join(worktreePath, "go.mod")) {
		return validateGoProject(worktreePath)
	}
	if fileExists(filepath.Join(worktreePath, "pyproject.toml")) || fileExists(filepath.Join(worktreePath, "setup.py")) {
		return validatePythonProject(worktreePath)
	}
	// No recognized project type — skip validation
	return nil
}

func validateNodeProject(dir string) error {
	// Try tsc --noEmit first (TypeScript), then build
	tscPath := filepath.Join(dir, "node_modules", ".bin", "tsc")
	if fileExists(tscPath) {
		cmd := exec.Command(tscPath, "--noEmit")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("TypeScript check failed:\n%s", truncateOutput(string(out), 500))
		}
		return nil
	}

	// Try npx vite build or npm run build
	cmd := exec.Command("npm", "run", "build")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm build failed:\n%s", truncateOutput(string(out), 500))
	}
	return nil
}

func validateGoProject(dir string) error {
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed:\n%s", truncateOutput(string(out), 500))
	}
	return nil
}

func validatePythonProject(dir string) error {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".py" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if strings.Contains(rel, ".venv") || strings.Contains(rel, "__pycache__") {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if len(files) == 0 {
		return nil
	}
	args := append([]string{"-m", "py_compile"}, files...)
	cmd := exec.Command("python3", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("python syntax check failed:\n%s", truncateOutput(string(out), 500))
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n...(truncated)"
}

// scanFileForConflictMarkers checks if a file contains unresolved merge conflict markers.
func scanFileForConflictMarkers(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "<<<<<<<") || strings.HasPrefix(line, "=======") || strings.HasPrefix(line, ">>>>>>>") {
			return true
		}
	}
	return false
}

// validateNoConflictMarkers scans all changed files for unresolved merge conflict markers.
func validateNoConflictMarkers(worktreePath string) []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD~1")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}

	var conflicts []string
	for _, relPath := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if relPath == "" {
			continue
		}
		absPath := filepath.Join(worktreePath, relPath)
		if scanFileForConflictMarkers(absPath) {
			conflicts = append(conflicts, relPath)
		}
	}
	return conflicts
}
