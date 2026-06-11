package engine

import (
	"sync"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/artifact"
	"github.com/tzone85/vortex-dispatch/internal/codegraph"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/notify"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Monitor polls running agents and progresses completed stories through
// review, QA, and merge.
type Monitor struct {
	registry         *runtime.Registry
	watchdog         *Watchdog
	reviewer         *Reviewer
	qa               *QA
	merger           *Merger
	conflictResolver *ConflictResolver
	config           config.Config
	eventStore       state.EventStore
	projStore        state.ProjectionStore
	escalation       *EscalationMachine
	manager          *Manager

	// artifactStore persists per-story artifacts (diffs, review results,
	// QA results) for post-mortem inspection.
	artifactStore *artifact.Store

	// checkpointPath is the file path for writing crash recovery checkpoints.
	// When set, the monitor writes phase-transition checkpoints before and
	// after the merge step so that resume can detect interrupted merges.
	checkpointPath string

	// reviewGate resolves the human-review mode for a requirement and
	// controls whether stories pause for approval before merging.
	reviewGate *ReviewGate

	// codeGraph enables blast-radius analysis before code review.
	// When set, the monitor runs detect-changes and passes the impact
	// analysis to the reviewer for richer context.
	codeGraph *codegraph.Runner

	// planner enables tier-3 (tech lead) re-planning. When set, the
	// monitor can decompose failing stories into smaller replacements.
	planner *Planner

	// dispatcher + executor allow the monitor to automatically spawn the
	// next wave of stories after merges complete, removing the need for
	// the user to manually run "vxd resume" between waves.
	dispatcher *Dispatcher
	executor   *Executor

	// docClient and docModel are used by the documentation generator
	// to create/update README.md after all stories are merged.
	docClient llm.Client
	docModel  string

	// dryRun causes the post-execution pipeline to simulate a successful
	// agent diff instead of checking the real worktree. This prevents
	// infinite retry loops when --dry-run agents produce no real changes.
	dryRun bool

	// mergeMu serializes the rebase-push-merge cycle so that each story
	// rebases onto the latest main before merging, preventing conflicts
	// when parallel agents touch the same files.
	mergeMu sync.Mutex

	// dagMu serializes DAG mutations (e.g. story splits) so that
	// concurrent pipelines don't corrupt the graph.
	dagMu sync.Mutex

	// slaStartTimes caches story start times (from STORY_STARTED event)
	// to avoid re-querying the event log on every poll cycle.
	slaStartTimes map[string]time.Time
	// slaBreachedSet tracks which stories have already emitted
	// STORY_SLA_BREACHED so we don't spam the event log.
	slaBreachedSet map[string]bool
	// slaMu protects the SLA tracking maps.
	slaMu sync.Mutex

	// notifier sends webhook notifications on SLA breaches and other
	// significant events. Defaults to NoopNotifier if not set.
	notifier notify.Notifier

	// lifecycle is the devdb.Lifecycle used to release ephemeral databases
	// after a successful merge. Nil means devdb is not enabled on the monitor.
	lifecycle *devdb.Lifecycle

	// techLeadFixer dispatches a focused fix story when the post-merge
	// integration build on main fails. Nil disables the feature.
	techLeadFixer *TechLeadFixer
}

// SetNotifier configures the outbound webhook notifier (Slack, Discord, etc.).
// If not called, notifications are silently dropped via NoopNotifier.
func (m *Monitor) SetNotifier(n notify.Notifier) { m.notifier = n }

// SetDryRun enables or disables dry-run mode on the monitor.
func (m *Monitor) SetDryRun(v bool) { m.dryRun = v }

// NewMonitor creates a Monitor wired to all pipeline components.
func NewMonitor(
	reg *runtime.Registry,
	wd *Watchdog,
	rev *Reviewer,
	qa *QA,
	merger *Merger,
	cfg config.Config,
	es state.EventStore,
	ps state.ProjectionStore,
) *Monitor {
	return &Monitor{
		registry:       reg,
		watchdog:       wd,
		reviewer:       rev,
		qa:             qa,
		merger:         merger,
		config:         cfg,
		eventStore:     es,
		projStore:      ps,
		escalation:     NewEscalationMachine(es, cfg.Routing),
		slaStartTimes:  make(map[string]time.Time),
		slaBreachedSet: make(map[string]bool),
	}
}

// SetArtifactStore enables per-story artifact persistence (diffs, reviews, QA).
func (m *Monitor) SetArtifactStore(store *artifact.Store) {
	m.artifactStore = store
}

// SetConflictResolver enables LLM-based automatic conflict resolution during
// rebase. Without this, rebase conflicts cause the story to be reset to draft.
func (m *Monitor) SetConflictResolver(cr *ConflictResolver) {
	m.conflictResolver = cr
}

// SetDocGenerator enables automatic README generation/update when all
// stories are merged. Uses the provided LLM client and model to generate
// documentation based on the implemented features.
func (m *Monitor) SetDocGenerator(client llm.Client, model string) {
	m.docClient = client
	m.docModel = model
}

// SetAutoResume enables automatic dispatch of the next wave when stories
// complete. Without this, the monitor exits after one wave and the user
// must manually run "vxd resume".
func (m *Monitor) SetAutoResume(d *Dispatcher, e *Executor) {
	m.dispatcher = d
	m.executor = e
}

// SetManager enables tier-2 (manager) escalation handling. When set, the
// monitor intercepts tier-2 stories before dispatch and routes them through
// the Manager for LLM-powered failure diagnosis and corrective actions.
func (m *Monitor) SetManager(mgr *Manager) {
	m.manager = mgr
}

// SetReviewGate enables human review gates. When set, the monitor checks
// the review mode for each story's requirement and pauses the pipeline
// for manual approval when the mode is "manual".
func (m *Monitor) SetReviewGate(rg *ReviewGate) {
	m.reviewGate = rg
}

// SetCodeGraph enables blast-radius analysis before code review.
// When set, the monitor runs detect-changes on the worktree and passes
// the impact analysis to the reviewer as additional context.
func (m *Monitor) SetCodeGraph(cg *codegraph.Runner) {
	m.codeGraph = cg
}

// SetCheckpointPath enables checkpoint writes for crash recovery.
func (m *Monitor) SetCheckpointPath(path string) {
	m.checkpointPath = path
}

// SetPlanner enables tier-3 (tech lead) re-planning. When set, the monitor
// can decompose failing stories into smaller replacement stories via the
// Planner's RePlan method.
func (m *Monitor) SetPlanner(p *Planner) {
	m.planner = p
}

// SetDevDBLifecycle wires a devdb.Lifecycle into the monitor so that it can
// release ephemeral databases after a successful merge. Pass nil to disable
// (the default).
func (m *Monitor) SetDevDBLifecycle(lc *devdb.Lifecycle) {
	m.lifecycle = lc
}

// HasDevDBLifecycle reports whether a devdb.Lifecycle has been configured.
// Used by tests to verify the lifecycle field is set correctly.
func (m *Monitor) HasDevDBLifecycle() bool {
	return m.lifecycle != nil
}

// SetTechLeadFixer enables post-merge integration build validation and
// automatic fix dispatch. When set, the monitor runs the project's build
// command after each successful merge and invokes the fixer if it fails.
func (m *Monitor) SetTechLeadFixer(f *TechLeadFixer) {
	m.techLeadFixer = f
}

// RunContext carries the state needed for auto-resume across waves.
type RunContext struct {
	ReqID          string
	PlannedStories []PlannedStory
	DAG            *graph.DAG
	WaveNumber     int
}
