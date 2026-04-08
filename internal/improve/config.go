package improve

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds all configuration for the self-improvement engine.
type Config struct {
	// API keys
	FirecrawlKey string
	ResendKey    string
	GoogleAIKey  string

	// Paths
	RepoPath string // VXD repository root
	AuditDir string // docs/self-improvement/

	// Guardrails
	MaxPRsPerRun         int
	MaxDiffLines         int
	MaxFilesChanged      int
	RelevanceThreshold   int
	MaxFindingsToAnalyze int

	// Email
	EmailTo   string
	EmailFrom string

	// Claude CLI
	ClaudePath string

	// Dry run mode
	DryRun bool

	// Paths (continued)
	OpportunitiesDir string // docs/opportunities/

	// Opportunity hunting
	ActiveBidding       bool
	MaxProposalsPerDay  int
	MinHourlyRate       int
	OpportunityKeywords [][]string // 7 sets, rotated daily
}

// AllowedLicenses lists permissive licenses acceptable for new dependencies.
var AllowedLicenses = map[string]bool{
	"Apache-2.0":  true,
	"MIT":         true,
	"BSD-2-Clause": true,
	"BSD-3-Clause": true,
	"ISC":         true,
	"MPL-2.0":     true,
}

// LoadConfig reads configuration from environment variables and applies defaults.
func LoadConfig() (Config, error) {
	firecrawlKey := os.Getenv("FIRECRAWL_API_KEY")
	if firecrawlKey == "" {
		return Config{}, fmt.Errorf("FIRECRAWL_API_KEY environment variable is required")
	}

	resendKey := os.Getenv("RESEND_API_KEY")
	if resendKey == "" {
		return Config{}, fmt.Errorf("RESEND_API_KEY environment variable is required")
	}

	googleAIKey := os.Getenv("GOOGLE_AI_API_KEY")
	if googleAIKey == "" {
		return Config{}, fmt.Errorf("GOOGLE_AI_API_KEY environment variable is required")
	}

	repoPath, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("determine working directory: %w", err)
	}

	claudePath := "claude"
	if cp := os.Getenv("CLAUDE_PATH"); cp != "" {
		claudePath = cp
	}

	activeBidding := os.Getenv("VXD_ACTIVE_BIDDING") == "true"

	maxProposals := 3
	if mp := os.Getenv("VXD_MAX_PROPOSALS_PER_DAY"); mp != "" {
		if v, err := strconv.Atoi(mp); err == nil && v > 0 {
			maxProposals = v
		}
	}

	minHourly := 50
	if mh := os.Getenv("VXD_MIN_HOURLY_RATE"); mh != "" {
		if v, err := strconv.Atoi(mh); err == nil && v > 0 {
			minHourly = v
		}
	}

	defaultKeywords := [][]string{
		{"software developer", "backend"},
		{"web application", "fullstack"},
		{"API development", "REST", "GraphQL"},
		{"automation", "scripting", "CLI"},
		{"AI", "machine learning", "LLM integration"},
		{"Python", "Django", "FastAPI"},
		{"React", "Next.js", "Node.js", "TypeScript"},
	}

	return Config{
		FirecrawlKey:         firecrawlKey,
		ResendKey:            resendKey,
		GoogleAIKey:          googleAIKey,
		RepoPath:             repoPath,
		AuditDir:             filepath.Join(repoPath, "docs", "self-improvement"),
		OpportunitiesDir:     filepath.Join(repoPath, "docs", "opportunities"),
		MaxPRsPerRun:         3,
		MaxDiffLines:         500,
		MaxFilesChanged:      10,
		RelevanceThreshold:   5,
		MaxFindingsToAnalyze: 10,
		EmailTo:              "vortex.dispatch01@gmail.com",
		EmailFrom:            "VXD Self-Improvement <onboarding@resend.dev>",
		ClaudePath:           claudePath,
		DryRun:               false,
		ActiveBidding:        activeBidding,
		MaxProposalsPerDay:   maxProposals,
		MinHourlyRate:        minHourly,
		OpportunityKeywords:  defaultKeywords,
	}, nil
}
