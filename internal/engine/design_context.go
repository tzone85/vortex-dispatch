package engine

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/figma"
	"github.com/tzone85/vortex-dispatch/internal/sanitize"
)

// maxDesignContextBytes bounds how much pulled design markdown rides into
// prompts — beyond this a design file is pathological, not informative.
const maxDesignContextBytes = 16 << 10

// loadDesignContext reads the Figma design context that `vxd req` pulled into
// <repo>/.vxd-design/, if any. The content embeds third-party data (Figma
// layer names), so it is scanned for prompt-injection patterns — a hit drops
// the context with a loud log rather than feeding an attack into the planner
// or the agents.
func loadDesignContext(repoDir string) string {
	data, err := os.ReadFile(filepath.Join(repoDir, figma.DirName, figma.ContextFileName))
	if err != nil || len(data) == 0 {
		return ""
	}
	if len(data) > maxDesignContextBytes {
		data = data[:maxDesignContextBytes]
	}
	content := string(data)
	if pattern := sanitize.MatchInjectionPattern(content); pattern != "" {
		log.Printf("[figma] design context at %s/%s contains a prompt-injection pattern (%q) — DROPPING it; inspect the Figma file's layer names", figma.DirName, figma.ContextFileName, pattern)
		return ""
	}
	// Neutralise angle brackets so a Figma layer literally named
	// "</design-reference>" cannot close the data framing the planner wraps
	// this content in. Escaping at this single choke point protects every
	// prompt that embeds the context.
	content = strings.ReplaceAll(content, "<", "&lt;")
	return content
}

// copyDesignDir copies <repo>/.vxd-design/ (context markdown + PNG renders)
// into the story worktree so the coding agent can open the reference images.
// Best-effort: a copy failure logs and the prompt still carries the markdown.
func copyDesignDir(repoDir, worktreePath string) {
	src := filepath.Join(repoDir, figma.DirName)
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	dst := filepath.Join(worktreePath, figma.DirName)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		log.Printf("[figma] create %s in worktree: %v", figma.DirName, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			log.Printf("[figma] copy %s: %v", e.Name(), err)
			continue
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			log.Printf("[figma] write %s to worktree: %v", e.Name(), err)
		}
	}
}
