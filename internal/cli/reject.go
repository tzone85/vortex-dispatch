package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newRejectPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject-plan <req-id> <feedback>",
		Short: "Reject a plan with feedback",
		Args:  cobra.ExactArgs(2),
		RunE:  runRejectPlan,
	}
}

func runRejectPlan(cmd *cobra.Command, args []string) error {
	reqID := args[0]
	feedback := args[1]

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	if _, err := s.Proj.GetRequirement(reqID); err != nil {
		return fmt.Errorf("requirement %s not found", reqID)
	}

	evt := state.NewEvent(state.EventPlanRejected, "human", "", map[string]any{
		"req_id":   reqID,
		"feedback": feedback,
	})
	if err := s.Events.Append(evt); err != nil {
		return fmt.Errorf("append plan-rejected event: %w", err)
	}
	if err := s.Proj.Project(evt); err != nil {
		return fmt.Errorf("project plan-rejected event: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Plan rejected for %s.\nFeedback: %s\nRe-run 'vxd req' with updated requirement.\n", reqID, feedback)
	return nil
}

func newRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject <story-id> <feedback>",
		Short: "Reject a story PR with feedback",
		Args:  cobra.ExactArgs(2),
		RunE:  runReject,
	}
}

func runReject(cmd *cobra.Command, args []string) error {
	storyID := args[0]
	feedback := args[1]

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	story, err := s.Proj.GetStory(storyID)
	if err != nil {
		return fmt.Errorf("story %s not found", storyID)
	}
	if story.Status != "awaiting_approval" {
		return fmt.Errorf("story %s is in status %q, not awaiting_approval", storyID, story.Status)
	}

	evt := state.NewEvent(state.EventStoryRejected, "human", storyID, map[string]any{
		"story_id": storyID,
		"feedback": feedback,
	})
	if err := s.Events.Append(evt); err != nil {
		return fmt.Errorf("append story-rejected event: %w", err)
	}
	if err := s.Proj.Project(evt); err != nil {
		return fmt.Errorf("project story-rejected event: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Rejected: %s\nFeedback: %s\nStory reset to draft for retry.\n", story.Title, feedback)
	return nil
}
