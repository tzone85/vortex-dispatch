package config

import (
	"strings"
	"testing"
)

// validAutoresearch returns a minimal AutoresearchConfig that passes
// validate(). Tests perturb single fields to exercise each guard.
func validAutoresearch() AutoresearchConfig {
	return AutoresearchConfig{
		Enabled:       true,
		Metric:        AutoresearchMetric{Command: "bench.sh", Parser: AutoresearchMetricParser{Kind: "regex", Pattern: "score=(.+)"}},
		EditablePaths: []string{"src/**/*.go"},
		Gate:          "auto",
		Budget:        "5m",
		Parallel:      4,
		Bayes:         AutoresearchBayes{PriorAlpha: 1, PriorBeta: 1},
	}
}

func TestAutoresearchValidate_DisabledIsAlwaysOK(t *testing.T) {
	a := AutoresearchConfig{Enabled: false} // every other field zero
	if err := a.validate(); err != nil {
		t.Errorf("disabled config should pass, got: %v", err)
	}
}

func TestAutoresearchValidate_BaseConfigIsValid(t *testing.T) {
	if err := validAutoresearch().validate(); err != nil {
		t.Fatalf("baseline config should validate, got: %v", err)
	}
}

func TestAutoresearchValidate_CatchesEachBoundary(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*AutoresearchConfig)
		wantMsg string
	}{
		{
			"missing metric command",
			func(a *AutoresearchConfig) { a.Metric.Command = "" },
			"metric.command is required",
		},
		{
			"invalid parser kind",
			func(a *AutoresearchConfig) { a.Metric.Parser.Kind = "bogus" },
			"parser.kind must be one of",
		},
		{
			"regex parser without pattern",
			func(a *AutoresearchConfig) {
				a.Metric.Parser.Kind = "regex"
				a.Metric.Parser.Pattern = ""
			},
			"pattern is required for regex parser",
		},
		{
			"json_path parser without pattern",
			func(a *AutoresearchConfig) {
				a.Metric.Parser.Kind = "json_path"
				a.Metric.Parser.Pattern = ""
			},
			"pattern is required for json_path parser",
		},
		{
			"negative tie epsilon",
			func(a *AutoresearchConfig) { a.Metric.TieEpsilon = -0.01 },
			"tie_epsilon must be >= 0",
		},
		{
			"no editable paths",
			func(a *AutoresearchConfig) { a.EditablePaths = nil },
			"editable_paths must contain at least one",
		},
		{
			"invalid gate",
			func(a *AutoresearchConfig) { a.Gate = "yolo" },
			"gate must be",
		},
		{
			"empty budget",
			func(a *AutoresearchConfig) { a.Budget = "" },
			"budget is required",
		},
		{
			"zero parallel",
			func(a *AutoresearchConfig) { a.Parallel = 0 },
			"parallel must be >= 1",
		},
		{
			"negative bayes alpha",
			func(a *AutoresearchConfig) { a.Bayes.PriorAlpha = -1 },
			"prior_alpha and prior_beta must be >= 0",
		},
		{
			"negative bayes beta",
			func(a *AutoresearchConfig) { a.Bayes.PriorBeta = -1 },
			"prior_alpha and prior_beta must be >= 0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := validAutoresearch()
			c.mutate(&a)
			err := a.validate()
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("error %q does not mention %q", err.Error(), c.wantMsg)
			}
		})
	}
}

func TestAutoresearchValidate_JSONPathHappyPath(t *testing.T) {
	a := validAutoresearch()
	a.Metric.Parser.Kind = "json_path"
	a.Metric.Parser.Pattern = "$.score"
	if err := a.validate(); err != nil {
		t.Errorf("json_path with pattern should validate, got: %v", err)
	}
}

func TestAutoresearchValidate_LastFloatNoPatternRequired(t *testing.T) {
	a := validAutoresearch()
	a.Metric.Parser.Kind = "last_float"
	a.Metric.Parser.Pattern = ""
	if err := a.validate(); err != nil {
		t.Errorf("last_float parser without pattern should validate, got: %v", err)
	}
}

func TestAutoresearchValidate_ExitCodeInverseNoPatternRequired(t *testing.T) {
	a := validAutoresearch()
	a.Metric.Parser.Kind = "exit_code_inverse"
	a.Metric.Parser.Pattern = ""
	if err := a.validate(); err != nil {
		t.Errorf("exit_code_inverse parser without pattern should validate, got: %v", err)
	}
}

func TestAutoresearchValidate_AllValidGates(t *testing.T) {
	for _, gate := range []string{"auto", "winning", "pr"} {
		t.Run(gate, func(t *testing.T) {
			a := validAutoresearch()
			a.Gate = gate
			if err := a.validate(); err != nil {
				t.Errorf("gate %q should validate, got: %v", gate, err)
			}
		})
	}
}
