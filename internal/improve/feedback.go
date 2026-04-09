package improve

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FeedbackEntry records a single outcome for Bayesian learning.
type FeedbackEntry struct {
	Type       string    `json:"type"`        // "proposal", "pr", "finding"
	Category   string    `json:"category"`    // e.g., "go_ecosystem", "security", "backend_api"
	Source     string    `json:"source"`      // e.g., "jobicy", "remotive", "hn"
	SkillSet   string    `json:"skill_set"`   // e.g., "Go,PostgreSQL,REST"
	PriceRange string    `json:"price_range"` // e.g., "$5K-$10K"
	Outcome    string    `json:"outcome"`     // "won", "lost", "merged", "rejected", "acted", "ignored"
	Timestamp  time.Time `json:"timestamp"`
}

// FeedbackLoop reads outcomes and computes adjusted scoring weights.
type FeedbackLoop struct {
	feedbackPath string // JSONL file
}

// SuccessRate holds empirical win/merge rates for a dimension.
type SuccessRate struct {
	Dimension string  `json:"dimension"` // e.g., "source:jobicy", "category:backend_api"
	Total     int     `json:"total"`
	Successes int     `json:"successes"`
	Rate      float64 `json:"rate"` // successes/total
}

// ScoringAdjustment represents how to adjust the base scoring.
type ScoringAdjustment struct {
	Dimension  string  `json:"dimension"`
	Multiplier float64 `json:"multiplier"` // >1 = boost, <1 = penalize
	Confidence float64 `json:"confidence"` // 0-1, based on sample size
}

// minSamplesForConfidence is the minimum number of samples needed before
// adjustments have any meaningful confidence.
const minSamplesForConfidence = 5

// highConfidenceSamples is the sample count at which confidence reaches 1.0.
const highConfidenceSamples = 30

// NewFeedbackLoop creates a FeedbackLoop reading from the given JSONL path.
func NewFeedbackLoop(feedbackPath string) *FeedbackLoop {
	return &FeedbackLoop{feedbackPath: feedbackPath}
}

// AppendFeedback logs a single outcome entry to the feedback JSONL file.
func (fl *FeedbackLoop) AppendFeedback(entry FeedbackEntry) error {
	if err := os.MkdirAll(filepath.Dir(fl.feedbackPath), 0o755); err != nil {
		return fmt.Errorf("create feedback dir: %w", err)
	}

	f, err := os.OpenFile(fl.feedbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open feedback file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal feedback entry: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write feedback entry: %w", err)
	}
	return f.Sync()
}

// ReadFeedback reads all entries from the feedback JSONL file.
// Returns nil, nil if the file does not exist (zero-data case).
func (fl *FeedbackLoop) ReadFeedback() ([]FeedbackEntry, error) {
	f, err := os.Open(fl.feedbackPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open feedback file: %w", err)
	}
	defer f.Close()

	var entries []FeedbackEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e FeedbackEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// ComputeSuccessRates groups feedback entries by dimension and computes
// empirical success rates. Dimensions include source, category, skill_set,
// and price_range.
func (fl *FeedbackLoop) ComputeSuccessRates() ([]SuccessRate, error) {
	entries, err := fl.ReadFeedback()
	if err != nil {
		return nil, err
	}
	return computeSuccessRatesFromEntries(entries), nil
}

// computeSuccessRatesFromEntries is the pure function that computes rates
// from a slice of entries, allowing easy testing without file I/O.
func computeSuccessRatesFromEntries(entries []FeedbackEntry) []SuccessRate {
	type counter struct {
		total     int
		successes int
	}
	dims := make(map[string]*counter)

	incr := func(dim string, isSuccess bool) {
		c, ok := dims[dim]
		if !ok {
			c = &counter{}
			dims[dim] = c
		}
		c.total++
		if isSuccess {
			c.successes++
		}
	}

	for _, e := range entries {
		isSuccess := isSuccessOutcome(e.Outcome)

		if e.Source != "" {
			incr("source:"+e.Source, isSuccess)
		}
		if e.Category != "" {
			incr("category:"+e.Category, isSuccess)
		}
		if e.SkillSet != "" {
			incr("skill_set:"+e.SkillSet, isSuccess)
		}
		if e.PriceRange != "" {
			incr("price_range:"+e.PriceRange, isSuccess)
		}
	}

	rates := make([]SuccessRate, 0, len(dims))
	for dim, c := range dims {
		rate := 0.0
		if c.total > 0 {
			rate = float64(c.successes) / float64(c.total)
		}
		rates = append(rates, SuccessRate{
			Dimension: dim,
			Total:     c.total,
			Successes: c.successes,
			Rate:      rate,
		})
	}

	// Sort for deterministic output
	sort.Slice(rates, func(i, j int) bool {
		return rates[i].Dimension < rates[j].Dimension
	})
	return rates
}

// isSuccessOutcome returns true for positive outcomes.
func isSuccessOutcome(outcome string) bool {
	switch outcome {
	case "won", "merged", "acted":
		return true
	default:
		return false
	}
}

// ComputeAdjustments turns success rates into scoring multipliers with
// confidence levels based on sample size. Returns nil when no data exists.
func (fl *FeedbackLoop) ComputeAdjustments() []ScoringAdjustment {
	rates, err := fl.ComputeSuccessRates()
	if err != nil || len(rates) == 0 {
		return nil
	}
	return computeAdjustmentsFromRates(rates)
}

// computeAdjustmentsFromRates is the pure function for computing adjustments.
func computeAdjustmentsFromRates(rates []SuccessRate) []ScoringAdjustment {
	adjustments := make([]ScoringAdjustment, 0, len(rates))

	for _, r := range rates {
		if r.Total < minSamplesForConfidence {
			continue // not enough data
		}

		confidence := math.Min(float64(r.Total)/float64(highConfidenceSamples), 1.0)

		// Convert rate to multiplier: 50% baseline rate.
		// rate > 0.5 boosts (max 1.5x), rate < 0.5 penalizes (min 0.5x).
		// The multiplier is blended toward 1.0 by (1 - confidence).
		rawMultiplier := 0.5 + r.Rate
		blended := 1.0 + (rawMultiplier-1.0)*confidence

		adjustments = append(adjustments, ScoringAdjustment{
			Dimension:  r.Dimension,
			Multiplier: blended,
			Confidence: confidence,
		})
	}

	sort.Slice(adjustments, func(i, j int) bool {
		return adjustments[i].Dimension < adjustments[j].Dimension
	})
	return adjustments
}

// AdjustOpportunityScore applies Bayesian adjustments to an opportunity's
// rank. It returns a new Opportunity with the adjusted rank (immutable).
// If no adjustments match, the opportunity is returned unchanged.
func (fl *FeedbackLoop) AdjustOpportunityScore(opp Opportunity, adjustments []ScoringAdjustment) Opportunity {
	if len(adjustments) == 0 {
		return opp
	}

	multiplier := 1.0
	matched := 0

	for _, adj := range adjustments {
		if matchesDimension(opp, adj.Dimension) {
			multiplier *= adj.Multiplier
			matched++
		}
	}

	if matched == 0 {
		return opp
	}

	adjusted := opp
	adjusted.Rank = int(math.Round(float64(opp.Rank) * multiplier))
	if adjusted.Rank < 0 {
		adjusted.Rank = 0
	}
	return adjusted
}

// matchesDimension checks if an opportunity matches a given dimension string.
func matchesDimension(opp Opportunity, dimension string) bool {
	parts := strings.SplitN(dimension, ":", 2)
	if len(parts) != 2 {
		return false
	}
	dimType, dimValue := parts[0], parts[1]

	switch dimType {
	case "source":
		return strings.EqualFold(opp.Source, dimValue)
	case "category":
		// Opportunities don't have a Category field directly, but skills
		// may approximate it. Skip for now.
		return false
	case "skill_set":
		return strings.EqualFold(strings.Join(opp.Skills, ","), dimValue)
	case "price_range":
		return strings.EqualFold(opp.Budget, dimValue)
	default:
		return false
	}
}

// GenerateInsights produces human-readable patterns from the feedback data
// suitable for MemPalace storage.
func (fl *FeedbackLoop) GenerateInsights() string {
	rates, err := fl.ComputeSuccessRates()
	if err != nil || len(rates) == 0 {
		return ""
	}
	return generateInsightsFromRates(rates)
}

// generateInsightsFromRates is the pure function for generating insight text.
func generateInsightsFromRates(rates []SuccessRate) string {
	var sb strings.Builder
	sb.WriteString("# Feedback Loop Insights\n\n")
	sb.WriteString("Generated: " + time.Now().Format("2006-01-02 15:04") + "\n\n")

	hasContent := false

	// Group by dimension type
	groups := make(map[string][]SuccessRate)
	for _, r := range rates {
		parts := strings.SplitN(r.Dimension, ":", 2)
		if len(parts) == 2 {
			groups[parts[0]] = append(groups[parts[0]], r)
		}
	}

	dimLabels := map[string]string{
		"source":      "By Source",
		"category":    "By Category",
		"skill_set":   "By Skill Set",
		"price_range": "By Price Range",
	}

	for _, dimType := range []string{"source", "category", "skill_set", "price_range"} {
		group, ok := groups[dimType]
		if !ok || len(group) == 0 {
			continue
		}

		label := dimLabels[dimType]
		sb.WriteString("## " + label + "\n\n")

		// Sort by rate descending
		sort.Slice(group, func(i, j int) bool {
			return group[i].Rate > group[j].Rate
		})

		for _, r := range group {
			parts := strings.SplitN(r.Dimension, ":", 2)
			value := r.Dimension
			if len(parts) == 2 {
				value = parts[1]
			}
			pct := r.Rate * 100
			confidence := "low"
			if r.Total >= highConfidenceSamples {
				confidence = "high"
			} else if r.Total >= minSamplesForConfidence {
				confidence = "medium"
			}
			sb.WriteString(fmt.Sprintf("- **%s**: %.0f%% success rate (%d/%d, %s confidence)\n",
				value, pct, r.Successes, r.Total, confidence))
			hasContent = true
		}
		sb.WriteString("\n")
	}

	if !hasContent {
		return ""
	}

	return sb.String()
}
