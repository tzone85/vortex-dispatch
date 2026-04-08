package improve_test

import (
	"os"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestLoadConfig_FromEnv(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "fc-test")
	t.Setenv("RESEND_API_KEY", "re-test")
	t.Setenv("GOOGLE_AI_API_KEY", "gai-test")

	cfg, err := improve.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.FirecrawlKey != "fc-test" {
		t.Errorf("expected FirecrawlKey 'fc-test', got %q", cfg.FirecrawlKey)
	}
	if cfg.ResendKey != "re-test" {
		t.Errorf("expected ResendKey 're-test', got %q", cfg.ResendKey)
	}
	if cfg.GoogleAIKey != "gai-test" {
		t.Errorf("expected GoogleAIKey 'gai-test', got %q", cfg.GoogleAIKey)
	}
}

func TestLoadConfig_MissingFirecrawlKey(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("RESEND_API_KEY", "re-test")
	t.Setenv("GOOGLE_AI_API_KEY", "gai-test")

	_, err := improve.LoadConfig()
	if err == nil {
		t.Fatal("expected error for missing FIRECRAWL_API_KEY")
	}
}

func TestLoadConfig_MissingResendKey(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "fc-test")
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("GOOGLE_AI_API_KEY", "gai-test")

	_, err := improve.LoadConfig()
	if err == nil {
		t.Fatal("expected error for missing RESEND_API_KEY")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "fc-test")
	t.Setenv("RESEND_API_KEY", "re-test")
	t.Setenv("GOOGLE_AI_API_KEY", "gai-test")

	cfg, _ := improve.LoadConfig()
	if cfg.MaxPRsPerRun != 3 {
		t.Errorf("expected MaxPRsPerRun 3, got %d", cfg.MaxPRsPerRun)
	}
	if cfg.MaxDiffLines != 500 {
		t.Errorf("expected MaxDiffLines 500, got %d", cfg.MaxDiffLines)
	}
	if cfg.MaxFilesChanged != 10 {
		t.Errorf("expected MaxFilesChanged 10, got %d", cfg.MaxFilesChanged)
	}
	if cfg.RelevanceThreshold != 5 {
		t.Errorf("expected RelevanceThreshold 5, got %d", cfg.RelevanceThreshold)
	}
	if cfg.MaxFindingsToAnalyze != 10 {
		t.Errorf("expected MaxFindingsToAnalyze 10, got %d", cfg.MaxFindingsToAnalyze)
	}
	if cfg.EmailTo != "vortex.dispatch01@gmail.com" {
		t.Errorf("expected EmailTo 'vortex.dispatch01@gmail.com', got %q", cfg.EmailTo)
	}
}

func TestConfig_RepoPath(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "fc-test")
	t.Setenv("RESEND_API_KEY", "re-test")
	t.Setenv("GOOGLE_AI_API_KEY", "gai-test")

	cfg, _ := improve.LoadConfig()
	cwd, _ := os.Getwd()
	if cfg.RepoPath != cwd {
		t.Errorf("expected RepoPath %q, got %q", cwd, cfg.RepoPath)
	}
}
