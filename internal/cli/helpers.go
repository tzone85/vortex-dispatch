package cli

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// stores bundles the event store and projection store opened from a config.
// Both must be closed by the caller.
type stores struct {
	Config     config.Config
	Events     state.EventStore
	Proj       *state.SQLiteStore
	ProjectDir string // e.g. ~/.vxd/projects/acme-corp-api
}

// loadStores loads configuration and opens both event and projection stores
// scoped to the current project. The caller is responsible for closing both
// stores.
//
// Project resolution order:
//  1. --project flag (explicit name)
//  2. VXD_PROJECT env var
//  3. Git repo detection from cwd
func loadStores(cmd *cobra.Command) (stores, error) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return stores{}, err
	}

	baseDir := expandHome(cfg.Workspace.StateDir)

	// Auto-migrate old flat layout on first run
	migrated, migrateErr := engine.MigrateOldLayout(baseDir)
	if migrateErr != nil {
		log.Printf("warning: migration failed: %v", migrateErr)
	}
	if migrated {
		log.Printf("Migrated existing VXD data to %s/projects/_legacy/", baseDir)
	}

	// Resolve project name
	projectName, err := resolveProject(cmd)
	if err != nil {
		return stores{}, fmt.Errorf("resolve project: %w", err)
	}

	projectDir := engine.ProjectDir(baseDir, projectName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return stores{}, fmt.Errorf("create project dir: %w", err)
	}

	// Write metadata if this is a new project (metadata.json does not exist)
	metaPath := filepath.Join(projectDir, "metadata.json")
	if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
		cwd, _ := os.Getwd()
		remoteURL := detectRemoteURL(cwd)
		meta := engine.ProjectMetadata{
			Name:         projectName,
			RepoPath:     cwd,
			RemoteURL:    remoteURL,
			CreatedAt:    time.Now().UTC(),
			LastActivity: time.Now().UTC(),
		}
		if writeErr := engine.WriteMetadata(projectDir, meta); writeErr != nil {
			log.Printf("warning: could not write project metadata: %v", writeErr)
		}
	}

	es, err := state.NewFileStore(filepath.Join(projectDir, "events.jsonl"))
	if err != nil {
		return stores{}, fmt.Errorf("open event store: %w", err)
	}

	ps, err := state.NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		es.Close()
		return stores{}, fmt.Errorf("open projection store: %w", err)
	}

	// Backfill acceptance_criteria for stories created before the column existed.
	allEvents, _ := es.List(state.EventFilter{Type: state.EventStoryCreated})
	ps.BackfillAcceptanceCriteria(allEvents)

	return stores{
		Config:     cfg,
		Events:     es,
		Proj:       ps,
		ProjectDir: projectDir,
	}, nil
}

// Close releases both stores.
func (s stores) Close() {
	if s.Events != nil {
		s.Events.Close()
	}
	if s.Proj != nil {
		s.Proj.Close()
	}
}

// resolveProject determines the project name from (in priority order):
//  1. --project flag
//  2. VXD_PROJECT environment variable
//  3. Git repository detection from cwd
func resolveProject(cmd *cobra.Command) (string, error) {
	// 1. Explicit --project flag
	if flagVal, _ := cmd.Flags().GetString("project"); flagVal != "" {
		return engine.SanitizeProjectName(flagVal), nil
	}

	// 2. VXD_PROJECT environment variable
	if envVal := os.Getenv("VXD_PROJECT"); envVal != "" {
		return engine.SanitizeProjectName(envVal), nil
	}

	// 3. Git detection from cwd
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	name, err := engine.ResolveProjectName(cwd)
	if err != nil {
		return "", fmt.Errorf("not in a git repository. Use --project or VXD_PROJECT env var")
	}
	return name, nil
}

// detectRemoteURL returns the git origin remote URL from the given directory,
// or an empty string if not available.
func detectRemoteURL(dir string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// loadConfig loads configuration using the chain: repo config -> global -> defaults.
func loadConfig(cfgPath string) (config.Config, error) {
	if cfgPath == "" {
		cfgPath = "vxd.yaml"
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Can't determine home, try repo config only then default
		cfg, loadErr := config.LoadFromFile(cfgPath)
		if loadErr != nil {
			return config.DefaultConfig(), nil
		}
		return cfg, nil
	}

	globalPath := filepath.Join(home, ".vxd", "config.yaml")
	return config.LoadConfigChain(cfgPath, globalPath)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if len(path) == 0 {
		return path
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
