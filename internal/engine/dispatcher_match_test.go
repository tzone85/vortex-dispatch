package engine

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestMatchesSequentialPattern_DirectMatch(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"go.mod", "package.json"},
			},
		},
	}

	if !d.matchesSequentialPattern("go.mod") {
		t.Error("expected go.mod to match")
	}
	if !d.matchesSequentialPattern("package.json") {
		t.Error("expected package.json to match")
	}
}

func TestMatchesSequentialPattern_NoMatch(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"go.mod", "package.json"},
			},
		},
	}

	if d.matchesSequentialPattern("main.go") {
		t.Error("expected main.go not to match")
	}
}

func TestMatchesSequentialPattern_GlobPattern(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"*.lock"},
			},
		},
	}

	if !d.matchesSequentialPattern("yarn.lock") {
		t.Error("expected yarn.lock to match *.lock")
	}
	if !d.matchesSequentialPattern("package-lock.json.lock") {
		t.Error("expected .lock extension to match")
	}
}

func TestMatchesSequentialPattern_PathWithBasename(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"go.mod"},
			},
		},
	}

	// filepath.Base("src/go.mod") is "go.mod", should match
	if !d.matchesSequentialPattern("src/go.mod") {
		t.Error("expected src/go.mod to match via basename")
	}
}

func TestMatchesSequentialPattern_EmptyPatterns(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: nil,
			},
		},
	}

	if d.matchesSequentialPattern("go.mod") {
		t.Error("expected no match with empty patterns")
	}
}

func TestMatchesSequentialPattern_EmptyFile(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"go.mod"},
			},
		},
	}

	if d.matchesSequentialPattern("") {
		t.Error("expected no match for empty file")
	}
}

func TestMatchesSequentialPattern_WildcardSubdir(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"*.yaml"},
			},
		},
	}

	// "config.yaml" as a basename should match "*.yaml"
	if !d.matchesSequentialPattern("deploy/config.yaml") {
		t.Error("expected deploy/config.yaml to match *.yaml via basename")
	}
}
