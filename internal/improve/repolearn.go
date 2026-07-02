package improve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LearningRepo describes a repository to study for patterns and ideas.
type LearningRepo struct {
	Name  string   // short name (e.g., "agentflow")
	URL   string   // git clone URL
	Focus []string // areas of interest (e.g., "testing", "escalation", "event-sourcing")
}

// Learning represents a single pattern or insight extracted from a studied repo.
type Learning struct {
	RepoName    string    `json:"repo_name"`
	Pattern     string    `json:"pattern"`
	Description string    `json:"description"`
	Relevance   int       `json:"relevance"` // 1-10
	Component   string    `json:"component"` // VXD component this could apply to
	Suggestion  string    `json:"suggestion"`
	ExtractedAt time.Time `json:"extracted_at"`
}

// learningRepos is the weekly rotation of repos to deep-clone and study.
// One repo per weekday (Mon-Fri), weekends off.
var learningRepos = []LearningRepo{
	{Name: "agentflow", URL: "https://github.com/shouc/agentflow", Focus: []string{"orchestration", "retry", "error-handling"}},
	{Name: "swe-agent", URL: "https://github.com/princeton-nlp/SWE-agent", Focus: []string{"agent-loop", "tool-use", "evaluation"}},
	{Name: "aider", URL: "https://github.com/paul-gauthier/aider", Focus: []string{"git-integration", "diff-editing", "model-selection"}},
	{Name: "openhands", URL: "https://github.com/All-Hands-AI/OpenHands", Focus: []string{"sandbox", "hooks", "agent-runtime"}},
	{Name: "autogen", URL: "https://github.com/microsoft/autogen", Focus: []string{"multi-agent", "conversation", "code-execution"}},
}

// LearningReposForDay returns the repos to study on the given day.
// Returns one repo per weekday (Mon-Fri), none on weekends.
func LearningReposForDay(day time.Time) []LearningRepo {
	wd := day.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return nil
	}
	// Map Monday=1..Friday=5 to index 0..4
	idx := (int(wd) - 1) % len(learningRepos)
	return []LearningRepo{learningRepos[idx]}
}

// RepoLearner clones or pulls tracked repos, detects new commits since last
// analysis, and extracts patterns via an LLM.
type RepoLearner struct {
	baseDir   string // directory where repos are cloned
	claudeCLI string // path to claude CLI (for LLM analysis)
	dryRun    bool
}

// NewRepoLearner creates a RepoLearner.
func NewRepoLearner(baseDir, claudeCLI string, dryRun bool) *RepoLearner {
	return &RepoLearner{
		baseDir:   baseDir,
		claudeCLI: claudeCLI,
		dryRun:    dryRun,
	}
}

// LearnFromRepo clones/pulls a repo, checks for new commits since the last
// baseline, and if there are changes, extracts learnings. Returns nil if
// there are no new commits or in dry-run mode.
func (rl *RepoLearner) LearnFromRepo(ctx context.Context, repo LearningRepo) ([]Learning, error) {
	repoDir := filepath.Join(rl.baseDir, repo.Name)

	// Clone or pull
	isNew, err := rl.ensureRepo(ctx, repo.URL, repoDir)
	if err != nil {
		return nil, fmt.Errorf("ensure repo %s: %w", repo.Name, err)
	}

	// Check baseline — skip if no new commits
	baselinePath := filepath.Join(rl.baseDir, repo.Name+".baseline")
	currentHead := gitHead(repoDir)
	previousHead := readBaseline(baselinePath)

	if !isNew && currentHead == previousHead {
		log.Printf("[repolearn] %s: no new commits since last analysis", repo.Name)
		return nil, nil
	}

	// Save new baseline
	if err := os.WriteFile(baselinePath, []byte(currentHead+"\n"), 0644); err != nil {
		log.Printf("[repolearn] warning: failed to write baseline for %s: %v", repo.Name, err)
	}

	if rl.dryRun {
		log.Printf("[repolearn] %s: dry run, skipping LLM analysis", repo.Name)
		return nil, nil
	}

	// Extract learnings via LLM
	learnings, err := rl.extractLearnings(ctx, repo, repoDir, previousHead)
	if err != nil {
		return nil, fmt.Errorf("extract learnings from %s: %w", repo.Name, err)
	}

	return learnings, nil
}

// ensureRepo clones the repo if it doesn't exist, or pulls if it does.
// Returns (isNew, error).
func (rl *RepoLearner) ensureRepo(ctx context.Context, url, repoDir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		// Clone
		log.Printf("[repolearn] Cloning %s into %s", url, repoDir)
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth=50", url, repoDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return false, fmt.Errorf("git clone: %w (%s)", err, truncateStr(string(out), 200))
		}
		return true, nil
	}

	// Pull
	log.Printf("[repolearn] Pulling %s", repoDir)
	cmd := exec.CommandContext(ctx, "git", "pull", "--ff-only")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		// Pull failure is non-fatal (detached HEAD, dirty worktree, etc.)
		log.Printf("[repolearn] pull warning: %v (%s)", err, truncateStr(string(out), 200))
	}
	return false, nil
}

// extractLearnings analyses changes since previousHead and asks the LLM to
// identify patterns relevant to VXD.
func (rl *RepoLearner) extractLearnings(ctx context.Context, repo LearningRepo, repoDir, previousHead string) ([]Learning, error) {
	// Get diff summary since last analysis
	var diffContent string
	if previousHead != "" {
		cmd := exec.CommandContext(ctx, "git", "log", previousHead+"..HEAD", "--stat", "--format=%s", "-20")
		cmd.Dir = repoDir
		out, err := cmd.Output()
		if err == nil {
			diffContent = truncateStr(string(out), 4000)
		}
	}
	if diffContent == "" {
		// Fallback: recent log
		cmd := exec.CommandContext(ctx, "git", "log", "--stat", "--format=%s", "-10")
		cmd.Dir = repoDir
		out, _ := cmd.Output()
		diffContent = truncateStr(string(out), 4000)
	}

	if diffContent == "" {
		return nil, nil
	}

	// Build LLM prompt
	focusAreas := "general architecture"
	if len(repo.Focus) > 0 {
		focusAreas = strings.Join(repo.Focus, ", ")
	}

	prompt := fmt.Sprintf(`Analyse these recent changes from the %s repository and extract patterns relevant to building an AI agent orchestration tool.

Focus areas: %s

Recent changes:
%s

For each relevant pattern, output a JSON array with objects containing:
- "pattern": short name (e.g., "Retry with exponential backoff")
- "description": 1-2 sentence description
- "relevance": 1-10 score (how applicable to an AI agent orchestrator)
- "component": which component this applies to (e.g., "engine/escalation", "runtime/execution", "state/events")
- "suggestion": concrete action to take (e.g., "Add exponential backoff to tier-0 retry in escalation.go")

Only include patterns with relevance >= 5. Output ONLY the JSON array.`, repo.Name, focusAreas, diffContent)

	// Call LLM via Claude CLI
	cmd := exec.CommandContext(ctx, rl.claudeCLI, "-p", prompt, "--output-format", "json") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- claudeCLI resolved from PATH; prompt is a single argv element, never shell-interpolated
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude CLI: %w", err)
	}

	// Parse response
	cleaned := extractJSONArray(string(out))
	var learnings []Learning
	if err := json.Unmarshal([]byte(cleaned), &learnings); err != nil {
		return nil, fmt.Errorf("parse learnings: %w (response: %s)", err, truncateStr(string(out), 200))
	}

	// Tag with metadata
	now := time.Now().UTC()
	for i := range learnings {
		learnings[i].RepoName = repo.Name
		learnings[i].ExtractedAt = now
	}

	return learnings, nil
}

// ConvertToFindings converts repo learnings into the self-improvement Finding
// format so they can flow into the existing changelog and email pipeline.
func ConvertToFindings(learnings []Learning) []Finding {
	findings := make([]Finding, 0, len(learnings))
	for _, l := range learnings {
		findings = append(findings, Finding{
			Title:     fmt.Sprintf("[%s] %s", l.RepoName, l.Pattern),
			Content:   fmt.Sprintf("%s\n\nSuggestion: %s\nComponent: %s", l.Description, l.Suggestion, l.Component),
			SourceURL: "repo:" + l.RepoName,
			Category:  "competitors",
			Direction: "historical",
			ScrapedAt: l.ExtractedAt,
		})
	}
	return findings
}

// StoreLearnings persists learnings to a dated JSONL file in the given directory.
func StoreLearnings(dir string, learnings []Learning) error {
	if len(learnings) == 0 {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create learnings dir: %w", err)
	}

	date := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, date+".jsonl")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open learnings file: %w", err)
	}
	defer f.Close()

	for _, l := range learnings {
		data, err := json.Marshal(l)
		if err != nil {
			continue
		}
		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("write learning: %w", err)
		}
		if _, err := f.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write learning newline: %w", err)
		}
	}
	return nil
}

// LoadLearnings reads a JSONL file of learnings.
func LoadLearnings(path string) ([]Learning, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var learnings []Learning
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var l Learning
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			continue
		}
		learnings = append(learnings, l)
	}
	return learnings, scanner.Err()
}

// gitHead returns the current HEAD commit hash for a repo directory.
func gitHead(repoDir string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// readBaseline reads the stored HEAD hash from a baseline file.
func readBaseline(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// extractJSONArray finds the first JSON array in a string, handling LLM
// responses that wrap the array in explanatory text.
func extractJSONArray(s string) string {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// truncateStr truncates a string to maxLen characters, appending "..." if cut.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
