package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newRetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retry <story-id>",
		Short: "Reset a story's escalation tier and re-queue it (transient-failure recovery)",
		Long: `Emit a STORY_RESET that clears the story's escalation history and returns it
to draft so it is re-dispatched from tier 0.

Use this when a story exhausted its escalation tiers for a TRANSIENT reason — a
429/session-limit storm, a temporary outage, a since-fixed model ID — rather than
a genuine quality failure. The escalation tier is recomputed from events scoped
to the latest reset, so a reset genuinely un-pins a story that would otherwise
re-pause at the top tier forever. Run 'vxd resume <req-id>' afterwards.`,
		Args: cobra.ExactArgs(1),
		RunE: runRetry,
	}
	cmd.Flags().String("reason", "manual retry (transient failure)", "reason recorded on the reset event")
	return cmd
}

func runRetry(cmd *cobra.Command, args []string) error {
	storyID := args[0]
	reason, _ := cmd.Flags().GetString("reason")

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	story, err := s.Proj.GetStory(storyID)
	if err != nil {
		return fmt.Errorf("story %s not found", storyID)
	}

	evt := state.NewEvent(state.EventStoryReset, "human", storyID, map[string]any{"reason": reason})
	if err := s.Events.Append(evt); err != nil {
		return fmt.Errorf("append reset event: %w", err)
	}
	if err := s.Proj.Project(evt); err != nil {
		return fmt.Errorf("project reset event: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Story %s reset: escalation tier cleared, status -> draft.\n", storyID)
	fmt.Fprintf(out, "Run 'vxd resume %s' to re-dispatch it.\n", story.ReqID)
	return nil
}
