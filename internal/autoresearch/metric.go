package autoresearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/shellexec"
)

// metricLastFloatRe extracts the last numeric token from arbitrary
// command output for the `last_float` metric kind. Hoisted to package
// level — ParseMetric is on the hot evaluation path and the old form
// re-compiled this regex on every call.
var metricLastFloatRe = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// Tiebreaker is the LLM-based judge invoked when two scores fall within
// `tie_epsilon` of each other. Returns a nudge in [-1, +1] which the
// harness scales by the configured epsilon.
type Tiebreaker interface {
	Tiebreak(ctx context.Context, rubric, candidateDiff string, candidateScore, baseline float64) (nudge float64, err error)
}

// MetricHarness executes the user's metric command in a worktree, parses
// the result, and (on near-tie) invokes the Tiebreaker to disambiguate.
type MetricHarness struct {
	Metric     config.AutoresearchMetric
	Tiebreaker Tiebreaker
}

// Measure runs the metric command in the given worktree, returning a Score.
// Lower-is-better is honoured by callers via Score.Improves; this layer
// returns the raw parsed value plus the tiebreak nudge if invoked.
func (h *MetricHarness) Measure(ctx context.Context, worktree string, baseline float64, candidateDiff string) (Score, error) {
	if h.Metric.Command == "" {
		return Score{}, errors.New("metric command is empty")
	}
	cmd := shellexec.CommandContext(ctx, h.Metric.Command)
	cmd.Dir = worktree
	out, runErr := cmd.CombinedOutput()
	exitCode := 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return Score{}, fmt.Errorf("metric exec: %w", runErr)
		}
	}

	primary, err := ParseMetric(h.Metric.Parser, string(out), exitCode)
	if err != nil {
		return Score{}, err
	}

	score := Score{
		Primary:       primary,
		Final:         primary,
		LowerIsBetter: h.Metric.Parser.LowerIsBetter,
		RawOutput:     string(out),
	}

	// Tiebreak only when within epsilon AND a tiebreaker is configured.
	if h.Metric.TieEpsilon > 0 && nearTie(primary, baseline, h.Metric.TieEpsilon) && h.Tiebreaker != nil {
		nudge, terr := h.Tiebreaker.Tiebreak(ctx, h.Metric.TiebreakRubric, candidateDiff, primary, baseline)
		if terr != nil {
			// Fail-closed: keep older baseline by reporting no improvement.
			// Direction-aware: bump by epsilon in the worse direction.
			if score.LowerIsBetter {
				score.TiebreakNudge = +h.Metric.TieEpsilon
			} else {
				score.TiebreakNudge = -h.Metric.TieEpsilon
			}
			score.Final = primary + score.TiebreakNudge
			return score, nil
		}
		nudge = clamp(nudge, -1, 1)
		// Nudge sign convention: positive nudge = "this candidate is better",
		// regardless of direction. Translate into a same-direction delta on
		// the primary score so Score.Improves() reads it correctly.
		if score.LowerIsBetter {
			score.TiebreakNudge = -nudge * h.Metric.TieEpsilon
		} else {
			score.TiebreakNudge = +nudge * h.Metric.TieEpsilon
		}
		score.Final = primary + score.TiebreakNudge
	}
	return score, nil
}

// ParseMetric extracts a numeric score from a metric command's output
// according to the configured parser. Pure function — testable without
// shell execution.
func ParseMetric(p config.AutoresearchMetricParser, output string, exitCode int) (float64, error) {
	switch p.Kind {
	case "regex":
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			return 0, fmt.Errorf("compile regex %q: %w", p.Pattern, err)
		}
		m := re.FindStringSubmatch(output)
		if len(m) < 2 {
			return 0, fmt.Errorf("regex %q matched no capture group in output", p.Pattern)
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
		if err != nil {
			return 0, fmt.Errorf("parse capture %q as float: %w", m[1], err)
		}
		return v, nil

	case "json_path":
		v, err := jsonPathFloat(output, p.Pattern)
		if err != nil {
			return 0, fmt.Errorf("json_path %q: %w", p.Pattern, err)
		}
		return v, nil

	case "last_float":
		all := metricLastFloatRe.FindAllString(output, -1)
		if len(all) == 0 {
			return 0, errors.New("last_float: no numeric token in output")
		}
		v, err := strconv.ParseFloat(all[len(all)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse last float %q: %w", all[len(all)-1], err)
		}
		return v, nil

	case "exit_code_inverse":
		if exitCode == 0 {
			return 1, nil
		}
		return 0, nil

	default:
		return 0, fmt.Errorf("unknown parser kind %q", p.Kind)
	}
}

// nearTie reports whether |score - baseline| <= epsilon * max(|baseline|, 1).
// The epsilon is interpreted as a relative fraction so it applies sanely
// across metric scales.
func nearTie(score, baseline, epsilon float64) bool {
	denom := absFloat(baseline)
	if denom < 1 {
		denom = 1
	}
	return absFloat(score-baseline)/denom <= epsilon
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// jsonPathFloat traverses a dotted path through a JSON document and returns
// the numeric value at that location. Supports a tiny subset:
//
//	  "a.b.c"        — nested object access
//	  "a.0.b"        — numeric segments index into arrays
//
// Numeric strings are coerced to float64; bools become 1/0; everything else errors.
func jsonPathFloat(jsonStr, path string) (float64, error) {
	var doc any
	if err := json.Unmarshal([]byte(jsonStr), &doc); err != nil {
		return 0, fmt.Errorf("invalid JSON: %w", err)
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				return 0, fmt.Errorf("key %q not present", seg)
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil {
				return 0, fmt.Errorf("non-numeric segment %q indexing array", seg)
			}
			if idx < 0 || idx >= len(v) {
				return 0, fmt.Errorf("index %d out of range", idx)
			}
			cur = v[idx]
		default:
			return 0, fmt.Errorf("cannot traverse %T at segment %q", v, seg)
		}
	}
	switch v := cur.(type) {
	case float64:
		return v, nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	}
	return 0, fmt.Errorf("value at path is %T, not numeric", cur)
}

// LLMTiebreaker is the default Tiebreaker implementation backed by an LLM client.
type LLMTiebreaker struct {
	Client llm.Client
	Model  string
}

const tiebreakSystem = `You are tiebreaking two near-identical experiment outcomes for an automated coding research harness.

Inputs you will receive:
- A rubric describing what "better" means for this repo.
- A candidate diff.
- The candidate's measured numeric score.
- The current baseline numeric score.

Output a single floating-point number in [-1, 1]:
- +1.0  ⇒ candidate is strongly preferred over baseline
-  0.0  ⇒ no meaningful preference
- -1.0  ⇒ candidate is strongly worse than baseline

Output ONLY the number on a single line. No explanation.`

// Tiebreak prompts the LLM with the rubric+diff and parses its single-number reply.
func (t *LLMTiebreaker) Tiebreak(ctx context.Context, rubric, diff string, score, baseline float64) (float64, error) {
	if t.Client == nil {
		return 0, errors.New("LLMTiebreaker: client is nil")
	}
	user := fmt.Sprintf("RUBRIC:\n%s\n\nBASELINE_SCORE: %v\nCANDIDATE_SCORE: %v\n\nDIFF:\n%s\n", rubric, baseline, score, diff)
	resp, err := t.Client.Complete(ctx, llm.CompletionRequest{
		Model:       t.Model,
		MaxTokens:   16,
		Temperature: 0,
		System:      tiebreakSystem,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: user},
		},
	})
	if err != nil {
		return 0, err
	}
	num, perr := strconv.ParseFloat(strings.TrimSpace(resp.Content), 64)
	if perr != nil {
		return 0, fmt.Errorf("tiebreak: cannot parse %q as float", resp.Content)
	}
	return clamp(num, -1, 1), nil
}
