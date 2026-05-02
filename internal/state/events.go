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
	EventStoryPRCreated       EventType = "STORY_PR_CREATED"
	EventStoryMerged          EventType = "STORY_MERGED"
	EventStoryEscalated       EventType = "STORY_ESCALATED"
	EventStoryRewritten       EventType = "STORY_REWRITTEN"
	EventStorySLABreached     EventType = "STORY_SLA_BREACHED"
	EventStorySplit           EventType = "STORY_SPLIT"

	// Agent lifecycle events.
	EventAgentSpawned    EventType = "AGENT_SPAWNED"
	EventAgentCheckpoint EventType = "AGENT_CHECKPOINT"
	EventAgentResumed    EventType = "AGENT_RESUMED"
	EventAgentStuck      EventType = "AGENT_STUCK"
	EventAgentTerminated EventType = "AGENT_TERMINATED"

	// Supervisor events.
	EventSupervisorCheck         EventType = "SUPERVISOR_CHECK"
	EventSupervisorReprioritize  EventType = "SUPERVISOR_REPRIORITIZE"
	EventSupervisorDriftDetected EventType = "SUPERVISOR_DRIFT_DETECTED"

	// Cleanup events.
	EventWorktreePruned EventType = "WORKTREE_PRUNED"
	EventBranchDeleted  EventType = "BRANCH_DELETED"
	EventGCCompleted    EventType = "GC_COMPLETED"

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
func NewEvent(eventType EventType, agentID, storyID string, data map[string]any) Event {
	var payload []byte
	if data != nil {
		payload, _ = json.Marshal(data)
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
