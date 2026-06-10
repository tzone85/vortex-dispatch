package state

// IsStoryComplete reports whether a story status is terminal for DAG
// dependency-resolution purposes. A story in one of these states is treated
// as "done" when computing which downstream stories are ready to dispatch.
//
// "awaiting_approval" is included because the PR has been submitted and the
// work is complete — only the human merge gate remains. Downstream stories can
// proceed (each branches from main; rebases handle conflicts).
//
// This function lives in the state package (not engine) so that sqlite.go
// can use it for state-machine guards without creating an import cycle
// (engine already imports state).
// terminalStatuses are the story statuses treated as "done" for DAG
// dependency-resolution. Kept as a single source of truth so SQL guards (e.g.
// guardedStartStory) and IsStoryComplete cannot drift apart.
var terminalStatuses = []string{"merged", "pr_submitted", "split", "awaiting_approval"}

func IsStoryComplete(status string) bool {
	for _, t := range terminalStatuses {
		if status == t {
			return true
		}
	}
	return false
}
