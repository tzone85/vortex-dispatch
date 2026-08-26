package engine_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// --------------------------------------------------------------------------
// F3 - Adaptive routing: pure-function tests
// --------------------------------------------------------------------------

func adaptiveEv(t state.EventType, story string, payload map[string]any) state.Event {
	return state.NewEvent(t, "tester", story, payload)
}

func adaptiveCreated(id string, cx int) state.Event {
	return adaptiveEv(state.EventStoryCreated, id, map[string]any{
		"id": id, "req_id": "r-1", "title": "t", "description": "d", "complexity": cx,
	})
}

func adaptiveStarted(id, role string) state.Event {
	return adaptiveEv(state.EventStoryStarted, id, map[string]any{"role": role})
}

func TestTierSuccessRates_FromEventHistory(t *testing.T) {
	cases := []struct {
		name   string
		events []state.Event
		want   map[int]map[int]engine.TierOutcome
	}{
		{
			name:   "empty history",
			events: nil,
			want:   map[int]map[int]engine.TierOutcome{},
		},
		{
			name: "single junior success",
			events: []state.Event{
				adaptiveCreated("s-1", 2),
				adaptiveStarted("s-1", "junior"),
				adaptiveEv(state.EventStoryCompleted, "s-1", nil),
			},
			want: map[int]map[int]engine.TierOutcome{
				engine.RouteTierJunior: {2: {Successes: 1}},
			},
		},
		{
			name: "escalation counts failure at old tier success at new",
			events: []state.Event{
				adaptiveCreated("s-2", 3),
				adaptiveStarted("s-2", "junior"),
				adaptiveEv(state.EventStoryEscalated, "s-2", map[string]any{"from_tier": 0, "to_tier": 1}),
				adaptiveStarted("s-2", "intermediate"),
				adaptiveEv(state.EventStoryCompleted, "s-2", nil),
			},
			want: map[int]map[int]engine.TierOutcome{
				engine.RouteTierJunior:       {3: {Failures: 1}},
				engine.RouteTierIntermediate: {3: {Successes: 1}},
			},
		},
		{
			name: "attempts aggregate per cell",
			events: []state.Event{
				adaptiveCreated("s-a", 2), adaptiveStarted("s-a", "junior"),
				adaptiveEv(state.EventStoryCompleted, "s-a", nil),
				adaptiveCreated("s-b", 2), adaptiveStarted("s-b", "junior"),
				adaptiveEv(state.EventStoryCompleted, "s-b", nil),
				adaptiveCreated("s-c", 2), adaptiveStarted("s-c", "junior"),
				adaptiveEv(state.EventStoryEscalated, "s-c", nil),
			},
			want: map[int]map[int]engine.TierOutcome{
				engine.RouteTierJunior: {2: {Successes: 2, Failures: 1}},
			},
		},
		{
			name: "attempt without started role is skipped",
			events: []state.Event{
				adaptiveCreated("s-d", 2),
				adaptiveEv(state.EventStoryCompleted, "s-d", nil),
			},
			want: map[int]map[int]engine.TierOutcome{},
		},
		{
			name: "attempt without created complexity is skipped",
			events: []state.Event{
				adaptiveStarted("s-e", "junior"),
				adaptiveEv(state.EventStoryCompleted, "s-e", nil),
			},
			want: map[int]map[int]engine.TierOutcome{},
		},
		{
			name: "unrelated event types are ignored",
			events: []state.Event{
				adaptiveEv(state.EventReqSubmitted, "", map[string]any{"id": "r-1"}),
				adaptiveEv(state.EventStoryAssigned, "s-f", map[string]any{"agent_id": "a"}),
			},
			want: map[int]map[int]engine.TierOutcome{},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := engine.TierSuccessRates(tc.events)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("TierSuccessRates() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func adaptiveRates(cells map[int]map[int][2]int) map[int]map[int]engine.TierOutcome {
	out := map[int]map[int]engine.TierOutcome{}
	for tier, m := range cells {
		out[tier] = map[int]engine.TierOutcome{}
		for cx, sf := range m {
			out[tier][cx] = engine.TierOutcome{Successes: sf[0], Failures: sf[1]}
		}
	}
	return out
}

func TestRecommendTier_PromoteDemoteBounds(t *testing.T) {
	base := config.DefaultConfig().Routing // min samples 5
	base.Adaptive = true                   // cases exercise the ENABLED path unless stated otherwise

	cases := []struct {
		name  string
		cx    int
		rates map[int]map[int]engine.TierOutcome
		cfg   config.RoutingConfig
		want  agent.Role
	}{
		{
			name:  "insufficient samples keeps default",
			cx:    2,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {2: {2, 0}}}), // 2 < 5
			want:  agent.RoleJunior,
		},
		{
			name:  "strong junior record demotes intermediate story",
			cx:    5,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {5: {5, 0}}}),
			want:  agent.RoleJunior,
		},
		{
			name:  "weak default tier promotes one level",
			cx:    3,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {3: {1, 4}}}), // 20% < 40%
			want:  agent.RoleIntermediate,
		},
		{
			name:  "weak senior never exceeds senior bound",
			cx:    8,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierSenior: {8: {1, 4}}}),
			want:  agent.RoleSenior,
		},
		{
			name: "disabled falls back to static routing despite strong history",
			cx:   5,
			cfg: func() config.RoutingConfig {
				c := base
				c.Adaptive = false
				return c
			}(),
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {5: {5, 0}}}),
			want:  agent.RoleIntermediate,
		},
		{
			name:  "exactly 80 percent qualifies for demote",
			cx:    5,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {5: {4, 1}}}),
			want:  agent.RoleJunior,
		},
		{
			name:  "below demote bar keeps default",
			cx:    5,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {5: {3, 2}}}), // 60%
			want:  agent.RoleIntermediate,
		},
		{
			name:  "promote from intermediate lands on senior one tier only",
			cx:    5,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierIntermediate: {5: {1, 4}}}),
			want:  agent.RoleSenior,
		},
		{
			name: "custom min samples raises the trust bar",
			cx:   5,
			cfg: func() config.RoutingConfig {
				c := base
				c.AdaptiveMinSamples = 10
				return c
			}(),
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {5: {5, 0}}}), // 5 < 10
			want:  agent.RoleIntermediate,
		},
		{
			name:  "demote wins when both promote and demote qualify",
			cx:    5,
			cfg:   base,
			rates: adaptiveRates(map[int]map[int][2]int{engine.RouteTierJunior: {5: {5, 0}}, engine.RouteTierIntermediate: {5: {1, 4}}}),
			want:  agent.RoleJunior,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := engine.RecommendTier(tc.cx, tc.rates, tc.cfg)
			if got != tc.want {
				t.Errorf("RecommendTier(%d) = %s, want %s", tc.cx, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// F3 - Wiring: the dispatcher ACTIVATES adaptive routing
// --------------------------------------------------------------------------

func mustProjectAdaptive(t *testing.T, ps state.ProjectionStore, evt state.Event) {
	t.Helper()
	if err := ps.Project(evt); err != nil {
		t.Fatalf("project %s/%s: %v", evt.Type, evt.StoryID, err)
	}
}

func mustAppendAdaptive(t *testing.T, es state.EventStore, evt state.Event) {
	t.Helper()
	if err := es.Append(evt); err != nil {
		t.Fatalf("append %s/%s: %v", evt.Type, evt.StoryID, err)
	}
}

// seedJuniorFailures writes n resolved junior attempts at complexity 2 that
// all escalated (i.e. failed) - the history adaptive routing learns from.
func seedJuniorFailures(t *testing.T, es state.EventStore, ps state.ProjectionStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("s-hist-%02d", i)
		// History lives in the EVENT store (what TierSuccessRates reads);
		// dispatchability lives in the projection.
		mustAppendAdaptive(t, es, adaptiveCreated(id, 2))
		mustProjectAdaptive(t, ps, adaptiveCreated(id, 2))
		mustAppendAdaptive(t, es, adaptiveStarted(id, "junior"))
		mustAppendAdaptive(t, es, adaptiveEv(state.EventStoryEscalated, id, map[string]any{
			"from_tier": 0, "to_tier": 1, "reason": "seeded failure",
		}))
	}
}

func TestDispatcher_AdaptiveRoutingWired(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	seedJuniorFailures(t, es, ps, 5)
	mustAppendAdaptive(t, es, adaptiveCreated("s-adaptive", 2))
	mustProjectAdaptive(t, ps, adaptiveCreated("s-adaptive", 2))

	cfg := config.DefaultConfig()
	cfg.Routing.Adaptive = true
	dispatcher := engine.NewDispatcher(cfg, es, ps)

	stories := []engine.PlannedStory{{ID: "s-adaptive", Title: "new story", Complexity: 2}}
	dag := graph.New()
	dag.AddNode("s-adaptive")

	assignments, err := dispatcher.DispatchWave(dag, map[string]bool{}, "r-adaptive", stories, 0)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}

	a := assignments[0]
	if a.Role != agent.RoleIntermediate {
		t.Errorf("WIRING FAILURE: adaptive routing should promote cx-2 story junior->intermediate after a 0/5 junior record, got %s", a.Role)
	}
	if !strings.Contains(a.AdaptiveDecision, "junior") || !strings.Contains(a.AdaptiveDecision, "intermediate") {
		t.Errorf("WIRING FAILURE: assignment missing adaptive decision annotation, got %q", a.AdaptiveDecision)
	}
}

func TestDispatcher_AdaptiveRouting_DisabledKeepsStatic(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	seedJuniorFailures(t, es, ps, 5)
	mustAppendAdaptive(t, es, adaptiveCreated("s-static", 2))
	mustProjectAdaptive(t, ps, adaptiveCreated("s-static", 2))

	dispatcher := engine.NewDispatcher(config.DefaultConfig(), es, ps) // Adaptive=false

	stories := []engine.PlannedStory{{ID: "s-static", Title: "new story", Complexity: 2}}
	dag := graph.New()
	dag.AddNode("s-static")

	assignments, err := dispatcher.DispatchWave(dag, map[string]bool{}, "r-adaptive", stories, 0)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].Role != agent.RoleJunior {
		t.Errorf("static routing should keep junior for cx 2, got %s", assignments[0].Role)
	}
	if assignments[0].AdaptiveDecision != "" {
		t.Errorf("disabled adaptive routing must not annotate a decision, got %q", assignments[0].AdaptiveDecision)
	}
}

func TestDispatcher_AdaptiveRouting_EscalationOverrideWins(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	seedJuniorFailures(t, es, ps, 5)
	mustAppendAdaptive(t, es, adaptiveCreated("s-esc", 2))
	mustProjectAdaptive(t, ps, adaptiveCreated("s-esc", 2))
	// This story itself already escalated to tier 1 (senior retry path).
	mustAppendAdaptive(t, es, adaptiveEv(state.EventStoryEscalated, "s-esc", map[string]any{
		"from_tier": 0, "to_tier": 1, "reason": "prior failure",
	}))

	cfg := config.DefaultConfig()
	cfg.Routing.Adaptive = true
	dispatcher := engine.NewDispatcher(cfg, es, ps)

	stories := []engine.PlannedStory{{ID: "s-esc", Title: "escalated story", Complexity: 2}}
	dag := graph.New()
	dag.AddNode("s-esc")

	assignments, err := dispatcher.DispatchWave(dag, map[string]bool{}, "r-adaptive", stories, 0)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].Role != agent.RoleSenior {
		t.Errorf("WIRING FAILURE: escalation chain must outrank adaptive refinement, got %s", assignments[0].Role)
	}
}

func TestDefaultConfig_AdaptiveOff(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Routing.Adaptive {
		t.Error("WIRING FAILURE: routing.adaptive must default to false (opt-in feature)")
	}
	if cfg.Routing.AdaptiveMinSamples != 5 {
		t.Errorf("routing.adaptive_min_samples default = %d, want 5", cfg.Routing.AdaptiveMinSamples)
	}
}