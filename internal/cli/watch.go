package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// terminalRequirementStatuses lets `vxd watch` know when to exit on its own —
// there is nothing more to follow once a requirement is done, failed, or
// archived. Each value is matched against Requirement.Status as projected
// from the event log.
var terminalRequirementStatuses = map[string]bool{
	"completed": true,
	"done":      true,
	"failed":    true,
	"archived":  true,
}

const watchPollInterval = 750 * time.Millisecond

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch [req-id]",
		Short: "Tail live events for a requirement (terminal-friendly)",
		Long: `Streams new events for one requirement, one line per event, until the
requirement reaches a terminal status or you press Ctrl+C.

With no argument, defaults to the most recent requirement submitted in the
current repository — so terminal users get an always-on status surface
without remembering any IDs.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runWatch,
	}
	cmd.Flags().Bool("all", false, "When resolving the default req-id, consider archived and other-repo requirements too")
	cmd.SilenceUsage = true
	return cmd
}

func runWatch(cmd *cobra.Command, args []string) error {
	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	out := cmd.OutOrStdout()

	reqID, err := resolveWatchReqID(cmd, s, args)
	if err != nil {
		return err
	}

	req, err := s.Proj.GetRequirement(reqID)
	if err != nil {
		return fmt.Errorf("get requirement: %w", err)
	}

	fmt.Fprintf(out, "Watching %s — %s\n", req.ID[:8], req.Title)
	fmt.Fprintf(out, "(status: %s; Ctrl+C to stop)\n\n", req.Status)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return tailRequirementEvents(ctx, out, s, reqID)
}

// resolveWatchReqID returns the requirement ID to follow. If args[0] is set
// the user is explicit; otherwise we pick the newest non-archived requirement
// for the current repo (matching the same default the `status` command uses).
func resolveWatchReqID(cmd *cobra.Command, s stores, args []string) (string, error) {
	if len(args) == 1 && args[0] != "" {
		return args[0], nil
	}

	showAll, _ := cmd.Flags().GetBool("all")

	var filter state.ReqFilter
	if !showAll {
		cwd, _ := os.Getwd()
		filter.RepoPath = cwd
		filter.ExcludeArchived = true
	}

	reqs, err := s.Proj.ListRequirementsFiltered(filter)
	if err != nil {
		return "", fmt.Errorf("list requirements: %w", err)
	}
	if len(reqs) == 0 {
		return "", fmt.Errorf("no requirement to watch in this repo. Run `vxd req \"<requirement>\"` first, or pass an explicit req-id")
	}

	// Newest first by CreatedAt — ListRequirementsFiltered does not
	// guarantee an order, so we sort defensively.
	sort.Slice(reqs, func(i, j int) bool {
		return reqs[i].CreatedAt.After(reqs[j].CreatedAt)
	})

	return reqs[0].ID, nil
}

// tailRequirementEvents prints one line per new event until ctx is done or
// the requirement reaches a terminal status. It polls the event store at
// watchPollInterval — the existing EventStore interface has no Tail method,
// and polling at <1 s is cheap (events.jsonl is appended sequentially, so
// the after-cursor narrows the scan).
func tailRequirementEvents(ctx context.Context, out interface {
	Write(p []byte) (n int, err error)
}, s stores, reqID string) error {
	cursor := time.Time{}
	seen := make(map[string]struct{})

	ticker := time.NewTicker(watchPollInterval)
	defer ticker.Stop()

	for {
		fresh, err := s.Events.List(state.EventFilter{After: cursor})
		if err != nil {
			return fmt.Errorf("list events: %w", err)
		}

		for _, evt := range fresh {
			if !eventMatchesReq(evt, reqID) {
				continue
			}
			if _, dup := seen[evt.ID]; dup {
				continue
			}
			seen[evt.ID] = struct{}{}
			fmt.Fprintln(out, formatWatchLine(evt))
			if evt.Timestamp.After(cursor) {
				cursor = evt.Timestamp
			}
		}

		// Check terminal status to know when to stop on our own.
		req, err := s.Proj.GetRequirement(reqID)
		if err == nil && terminalRequirementStatuses[req.Status] {
			fmt.Fprintf(out, "\n%s reached terminal status: %s\n", reqID[:8], req.Status)
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// eventMatchesReq returns true iff evt belongs to the watched requirement.
// Story events are matched by their ID namespace: story IDs start with
// engine.StoryIDPrefix(reqID) — sha256(reqID)[:8] for real (>8-char) reqIDs,
// the reqID verbatim for short test fixtures. Comparing raw reqID prefixes
// here would silently drop every story event in production. Requirement-level
// events (REQ_*) carry no StoryID; they are matched by the req_id payload field.
func eventMatchesReq(evt state.Event, reqID string) bool {
	prefix := engine.StoryIDPrefix(reqID)
	if evt.StoryID != "" && (evt.StoryID == prefix || strings.HasPrefix(evt.StoryID, prefix+"-")) {
		return true
	}
	if len(evt.Payload) > 0 {
		// Requirement-level payload keys are not uniform across emitters:
		// REQ_SUBMITTED/REQ_COMPLETED/REQ_BLOCKED carry "id", the planning
		// heartbeat and story-adjacent events carry "req_id". Accept both;
		// exact equality with the full reqID cannot collide with story IDs
		// (those are always <8-char-prefix>-<suffix>).
		payload := state.DecodePayload(evt.Payload)
		for _, key := range []string{"req_id", "id"} {
			if id, ok := payload[key].(string); ok && id == reqID {
				return true
			}
		}
	}
	return false
}

// formatWatchLine renders one event into a compact single line.
func formatWatchLine(evt state.Event) string {
	ts := evt.Timestamp.Format("15:04:05")
	tail := ""
	if evt.StoryID != "" {
		tail = " story=" + evt.StoryID
	}
	if evt.AgentID != "" {
		tail += " agent=" + evt.AgentID
	}
	return fmt.Sprintf("  %s %s%s", ts, evt.Type, tail)
}
