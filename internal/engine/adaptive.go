package engine

import (
	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Adaptive routing (F3): the event log already knows which execution tier
// succeeds at which complexity in THIS repo. These pure functions distill
// STORY_COMPLETED / STORY_ESCALATED history into per-(tier, complexity)
// outcomes and recommend a tier override of the static complexity thresholds.
// The dispatcher wires them in when routing.adaptive is enabled; see
// docs/adaptive-routing.md.

// Dispatch tiers used by adaptive routing, ordered cheapest to most capable.
// These are deliberately DISTINCT from the 5-tier escalation-chain numbers
// carried in STORY_ESCALATED / STORY_STARTED payloads (where junior and
// intermediate both sit at tier 0 and senior at tier 1): conflating the two
// scales would misattribute senior attempts to intermediate.
const (
	RouteTierJunior       = 0
	RouteTierIntermediate = 1
	RouteTierSenior       = 2
)

const (
	// adaptiveDemoteRate: a cheaper tier whose success rate at a complexity
	// meets this bar (with enough samples) is trusted with the story.
	adaptiveDemoteRate = 0.80
	// adaptiveRouteUpRate: the default tier below this success rate at a
	// complexity (with enough samples) is routed up one tier.
	adaptiveRouteUpRate = 0.40
	// adaptiveDefaultMinSamples is used when routing.adaptive_min_samples is
	// unset or non-positive.
	adaptiveDefaultMinSamples = 5
)

// TierOutcome aggregates resolved attempts for one (tier, complexity) cell.
// Counts are kept alongside the rate because RecommendTier needs the sample
// size (>= adaptive_min_samples) before trusting history - a bare rate map
// would lose that signal.
type TierOutcome struct {
	Successes int
	Failures  int
}

// Total returns the number of resolved samples behind this outcome.
func (o TierOutcome) Total() int { return o.Successes + o.Failures }

// Rate returns the success fraction (0 when there are no samples).
func (o TierOutcome) Rate() float64 {
	if o.Total() == 0 {
		return 0
	}
	return float64(o.Successes) / float64(o.Total())
}

// roleForRouteTier maps a dispatch tier back to its agent role.
func roleForRouteTier(tier int) (agent.Role, bool) {
	switch tier {
	case RouteTierJunior:
		return agent.RoleJunior, true
	case RouteTierIntermediate:
		return agent.RoleIntermediate, true
	case RouteTierSenior:
		return agent.RoleSenior, true
	}
	return "", false
}

// routeTierForRole maps an agent role to its dispatch tier.
func routeTierForRole(r agent.Role) (int, bool) {
	switch r {
	case agent.RoleJunior:
		return RouteTierJunior, true
	case agent.RoleIntermediate:
		return RouteTierIntermediate, true
	case agent.RoleSenior:
		return RouteTierSenior, true
	}
	return 0, false
}

// TierSuccessRates computes per-(dispatch-tier, complexity) success outcomes
// from the event history. Attribution rules, walked in log order:
//
//   - STORY_CREATED supplies the story's complexity.
//   - STORY_STARTED supplies the attempting role (emitted by the executor on
//     every spawn, including retries, so the latest one wins).
//   - STORY_ESCALATED records a FAILURE for the current role: an escalation
//     is proof the attempt at the current tier did not succeed.
//   - STORY_COMPLETED records a SUCCESS for the current role.
//
// Attempts with no resolvable role or complexity are skipped rather than
// guessed. Events are read straight from the store by the dispatcher; this
// function stays pure so the attribution rules are unit-testable.
func TierSuccessRates(events []state.Event) map[int]map[int]TierOutcome {
	complexityByStory := map[string]int{}
	currentRole := map[string]agent.Role{}
	out := map[int]map[int]TierOutcome{}

	record := func(tier, complexity int, success bool) {
		if _, ok := roleForRouteTier(tier); !ok {
			return
		}
		cell := out[tier]
		if cell == nil {
			cell = map[int]TierOutcome{}
			out[tier] = cell
		}
		o := cell[complexity]
		if success {
			o.Successes++
		} else {
			o.Failures++
		}
		cell[complexity] = o
	}

	for _, evt := range events {
		switch evt.Type {
		case state.EventStoryCreated:
			p := state.DecodePayload(evt.Payload)
			if evt.StoryID != "" {
				complexityByStory[evt.StoryID] = adaptivePayloadInt(p, "complexity")
			}
		case state.EventStoryStarted:
			p := state.DecodePayload(evt.Payload)
			if r, ok := p["role"].(string); ok && evt.StoryID != "" && r != "" {
				currentRole[evt.StoryID] = agent.Role(r)
			}
		case state.EventStoryEscalated:
			// Escalation means the attempt at the current role failed.
			if cx := complexityByStory[evt.StoryID]; cx > 0 {
				if tier, ok := routeTierForRole(currentRole[evt.StoryID]); ok {
					record(tier, cx, false)
				}
			}
		case state.EventStoryCompleted:
			if cx := complexityByStory[evt.StoryID]; cx > 0 {
				if tier, ok := routeTierForRole(currentRole[evt.StoryID]); ok {
					record(tier, cx, true)
				}
			}
		}
	}
	return out
}

// adaptivePayloadInt tolerates both float64 (JSON round-trip) and int payloads.
func adaptivePayloadInt(p map[string]any, key string) int {
	switch n := p[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// RecommendTier returns the agent role that should execute a story of the
// given complexity, refining the static complexity thresholds with history:
//
//   - adaptive disabled (or no qualifying history): the static default tier.
//   - demote: the CHEAPEST cheaper tier with >= minSamples resolved attempts
//     and >= 80% success at this complexity takes the story (checked first -
//     when both signals fire, the cheaper tier's strong record wins and saves
//     the most cost).
//   - promote: the default tier with >= minSamples attempts and < 40% success
//     routes up ONE tier (bounded: never above senior).
func RecommendTier(complexity int, rates map[int]map[int]TierOutcome, cfg config.RoutingConfig) agent.Role {
	def := agent.RouteByComplexity(complexity, cfg)
	if !cfg.Adaptive {
		return def
	}

	minSamples := cfg.AdaptiveMinSamples
	if minSamples <= 0 {
		minSamples = adaptiveDefaultMinSamples
	}

	defTier, _ := routeTierForRole(def)

	// Demote first: strongest cost win, and a >=80% cheaper-tier record
	// dominates a weak default tier when both rules qualify.
	for tier := RouteTierJunior; tier < defTier; tier++ {
		o := rates[tier][complexity]
		if o.Total() >= minSamples && o.Rate() >= adaptiveDemoteRate {
			r, _ := roleForRouteTier(tier)
			return r
		}
	}

	// Promote one tier when the default tier is burning escalations.
	if defTier < RouteTierSenior {
		o := rates[defTier][complexity]
		if o.Total() >= minSamples && o.Rate() < adaptiveRouteUpRate {
			up := defTier + 1
			if up > RouteTierSenior {
				up = RouteTierSenior
			}
			r, _ := roleForRouteTier(up)
			return r
		}
	}

	return def
}
