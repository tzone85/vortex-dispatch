package engine

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// capacityPauseReason builds the standardized pause reason for a transient
// capacity / session-limit exhaustion at a given pipeline stage. The "capacity
// limit" phrasing routes pauseResumeHint to the correct operator guidance.
func capacityPauseReason(stage string, err error) string {
	return fmt.Sprintf("transient LLM capacity/network error during %s — resume after it clears: %v", stage, err)
}

// pauseIfCapacity inspects an LLM-call error. If it is a transient capacity
// exhaustion (429 rate/session limit, 529 overloaded), it pauses the
// requirement cleanly and returns true — WITHOUT consuming an escalation
// attempt or advancing the tier, since the failure has nothing to do with the
// story's quality and will succeed once the limit resets.
//
// Returns false for every other error so the caller's normal handling runs.
func (m *Monitor) pauseIfCapacity(storyID, stage string, err error) bool {
	if !llm.IsCapacityError(err) {
		return false
	}
	log.Printf("[pipeline] capacity/session limit during %s for %s — pausing without escalation: %v",
		stage, storyID, err)
	m.pauseRequirement(storyID, capacityPauseReason(stage, err))
	return true
}

// agentLogHasCapacityError reports whether the coding agent's session log for a
// story contains a capacity/session-limit signature. The agent runs inside its
// own claude session; when it is rate/session-limited it produces no code and
// exits, so the only evidence is the result envelope captured in its log.
// Without this, a session-limited agent looks identical to a lazy agent and the
// story is wrongly escalated as "produced no code changes".
func (m *Monitor) agentLogHasCapacityError(storyID string) bool {
	stateDir := execExpandHome(m.config.Workspace.StateDir)
	logPath := filepath.Join(stateDir, "logs", storyID+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return false // no log / unreadable — fall back to normal handling
	}
	return llm.ContainsCapacitySignature(string(data))
}
