package state

import (
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
)

// EventType represents the type of a domain event in the system.
type EventType string

const (
	// Request lifecycle events.
	EventReqSubmitted EventType = "REQ_SUBMITTED"
	EventReqAnalyzed  EventType = "REQ_ANALYZED"
	EventReqPlanned   EventType = "REQ_PLANNED"
	EventReqPaused    EventType = "REQ_PAUSED"
	EventReqResumed   EventType = "REQ_RESUMED"
	EventReqCompleted EventType = "REQ_COMPLETED"
	EventReqBlocked   EventType = "REQ_BLOCKED"
	EventReqEstimated EventType = "REQ_ESTIMATED"

	// Story lifecycle events.
	EventStoryCreated         EventType = "STORY_CREATED"
	EventStoryEstimated       EventType = "STORY_ESTIMATED"
	EventStoryAssigned        EventType = "STORY_ASSIGNED"
	EventStoryStarted         EventType = "STORY_STARTED"
	EventStoryProgress        EventType = "STORY_PROGRESS"
	EventStoryCompleted       EventType = "STORY_COMPLETED"
	EventStoryReviewRequested EventType = "STORY_REVIEW_REQUESTED"
	EventStoryReviewPassed    EventType = "STORY_REVIEW_PASSED"
	EventStoryReviewFailed    EventType = "STORY_REVIEW_FAILED"
	EventStoryQAStarted       EventType = "STORY_QA_STARTED"
	EventStoryQAPassed        EventType = "STORY_QA_PASSED"
	EventStoryQAFailed        EventType = "STORY_QA_FAILED"
	EventStoryQAFlaky         EventType = "STORY_QA_FLAKY" // test step failed then passed on retry (qa.flaky_retries)
	EventStorySecurityPassed  EventType = "STORY_SECURITY_PASSED"
	EventStorySecurityFailed  EventType = "STORY_SECURITY_FAILED"
	EventStoryPRCreated       EventType = "STORY_PR_CREATED"
	EventStoryMerged          EventType = "STORY_MERGED"
	EventStoryEscalated       EventType = "STORY_ESCALATED"
	EventStoryRewritten       EventType = "STORY_REWRITTEN"
	EventStorySLABreached     EventType = "STORY_SLA_BREACHED"
	EventStorySplit           EventType = "STORY_SPLIT"
	EventStoryDBCreated       EventType = "STORY_DB_CREATED"
	EventStoryDBFailed        EventType = "STORY_DB_FAILED"
	EventStoryDBDeleted       EventType = "STORY_DB_DELETED"

	// Agent lifecycle events.
	EventAgentSpawned    EventType = "AGENT_SPAWNED"
	EventAgentCheckpoint EventType = "AGENT_CHECKPOINT"
	EventAgentResumed    EventType = "AGENT_RESUMED"
	EventAgentStuck      EventType = "AGENT_STUCK"
	EventAgentTerminated EventType = "AGENT_TERMINATED"

	// Supervisor events.
	EventSupervisorCheck         EventType = "SUPERVISOR_CHECK"
	EventSupervisorDriftDetected EventType = "SUPERVISOR_DRIFT_DETECTED"
	// EventSupervisorReprioritize was defined but never emitted anywhere in the
	// codebase and has been removed to prevent dead-code accumulation.
	// See: Task 10, Wave B wiring audit (2026-05-11).

	// Cleanup events.
	EventWorktreePruned EventType = "WORKTREE_PRUNED"
	EventBranchDeleted  EventType = "BRANCH_DELETED"
	EventGCCompleted    EventType = "GC_COMPLETED"

	// Security agent events.
	EventSecurityScanCompleted EventType = "SECURITY_SCAN_COMPLETED"
	EventSecurityRuleLearned   EventType = "SECURITY_RULE_LEARNED"

	// Review gate events.
	EventReviewModeSet         EventType = "REVIEW_MODE_SET"
	EventPlanApproved          EventType = "PLAN_APPROVED"
	EventPlanRejected          EventType = "PLAN_REJECTED"
	EventStoryAwaitingApproval EventType = "STORY_AWAITING_APPROVAL"
	EventStoryApproved         EventType = "STORY_APPROVED"
	EventStoryRejected         EventType = "STORY_REJECTED"

	// Recovery events.
	EventStoryReset        EventType = "STORY_RESET"
	EventRecoveryCompleted EventType = "RECOVERY_COMPLETED"

	// Conflict resolution events.
	EventStoryConflictBinary        EventType = "STORY_CONFLICT_BINARY"         // binary file handled without LLM
	EventStoryConflictBinaryRemoved EventType = "STORY_CONFLICT_BINARY_REMOVED" // oversized/compiled binary removed from branch
	EventStoryConflictEscalated     EventType = "STORY_CONFLICT_ESCALATED"      // conflict escalated to Tech Lead

	// Post-merge integration build events.
	EventStoryIntegrationFailed EventType = "STORY_INTEGRATION_FAILED" // main branch failed to build after merge

	// Planning lifecycle events.
	EventReqPlanningStarted EventType = "REQ_PLANNING_STARTED" // Tech Lead LLM call about to begin

	// Pipeline-health events.
	EventPipelineStalled EventType = "PIPELINE_STALLED" // unfinished stories exist but none are dispatchable (all tiers exhausted)

	// Operational security events.
	EventDashboardTokenRotated EventType = "DASHBOARD_TOKEN_ROTATED" // dashboard bearer token rotated (TTL expiry or `vxd dashboard rotate-token`)

	// Cost-tracking events (F2). STORY_COST_RECORDED lands once per measured
	// LLM call (stage: agent|review|planning|conflict|diagnosis) with raw token
	// usage and an estimated USD cost (0 for subscription-mode CLI calls).
	// REQ_BUDGET_EXCEEDED fires when a requirement's accumulated est_usd
	// crosses billing.max_usd_per_req; the accompanying REQ_PAUSED performs the
	// actual status transition (same clean-pause path as a capacity pause).
	EventStoryCostRecorded EventType = "STORY_COST_RECORDED"
	EventReqBudgetExceeded EventType = "REQ_BUDGET_EXCEEDED"

	// Autoresearch harness events. See docs/superpowers/specs/2026-05-02-autoresearch-harness-design.md.
	EventBaselineMeasured    EventType = "BASELINE_MEASURED"
	EventExperimentProposed  EventType = "EXPERIMENT_PROPOSED"
	EventExperimentRunning   EventType = "EXPERIMENT_RUNNING"
	EventExperimentMeasured  EventType = "EXPERIMENT_MEASURED"
	EventExperimentTiebroken EventType = "EXPERIMENT_TIEBROKEN"
	EventExperimentTripwired EventType = "EXPERIMENT_TRIPWIRED"
	EventExperimentKept      EventType = "EXPERIMENT_KEPT"
	EventExperimentDiscarded EventType = "EXPERIMENT_DISCARDED"
	EventExperimentFailed    EventType = "EXPERIMENT_FAILED"
	EventCoordinatorPanic    EventType = "COORDINATOR_PANIC"
	EventProgrammdEvolved    EventType = "PROGRAMMD_EVOLVED"
)

// Event represents a single domain event in the append-only event store.
type Event struct {
	ID        string    `json:"id"`
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	AgentID   string    `json:"agent_id"`
	StoryID   string    `json:"story_id,omitempty"`
	Payload   []byte    `json:"payload,omitempty"`
}

// DecodePayload unmarshals a JSON-encoded event payload into a map.
// Returns an empty map if the payload is nil or cannot be decoded.
func DecodePayload(payload []byte) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// NewEvent creates a new Event with a ULID identifier and current timestamp.
// If data is nil, the payload will be nil (not an empty JSON object).
// A single time.Now() call is used for both the ULID and the event Timestamp
// to prevent clock skew between the two.
//
// If json.Marshal(data) fails (cycle, unsupported type, NaN float, etc.)
// the payload is replaced with a stub recording the marshal error rather
// than left nil — a nil payload would silently lose the event's data on
// projection without any signal that it ever existed. The stub keeps the
// event readable downstream while making the corruption visible in logs
// and dashboards.
func NewEvent(eventType EventType, agentID, storyID string, data map[string]any) Event {
	var payload []byte
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			// Best-effort stub. Encoded by hand so we never recurse
			// back into json.Marshal with the same poisoned data.
			payload = []byte(`{"_marshal_error":` + jsonString(err.Error()) + `}`)
		} else {
			payload = raw
		}
	}

	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Timestamp(now), rand.Reader)

	return Event{
		ID:        id.String(),
		Type:      eventType,
		Timestamp: now,
		AgentID:   agentID,
		StoryID:   storyID,
		Payload:   payload,
	}
}

// jsonString returns a minimally-escaped JSON string literal. Used only
// from NewEvent's marshal-failure stub where json.Marshal itself is
// suspect — we cannot trust json.Marshal(err.Error()) to round-trip the
// same byte stream after a panic in the encoder. The escape set covers
// the ones json.Marshal would: backslash, double-quote, control bytes.
func jsonString(s string) string {
	var b []byte
	b = append(b, '"')
	for _, r := range s {
		switch r {
		case '\\':
			b = append(b, '\\', '\\')
		case '"':
			b = append(b, '\\', '"')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			if r < 0x20 {
				continue // skip other control bytes
			}
			b = append(b, []byte(string(r))...)
		}
	}
	b = append(b, '"')
	return string(b)
}
