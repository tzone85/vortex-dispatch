package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newApprovePlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve-plan <req-id>",
		Short: "Approve a requirement plan for dispatch",
		Args:  cobra.ExactArgs(1),
		RunE:  runApprovePlan,
	}
}

func runApprovePlan(cmd *cobra.Command, args []string) error {
	reqID := args[0]
	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	if _, err := s.Proj.GetRequirement(reqID); err != nil {
		return fmt.Errorf("requirement %s not found", reqID)
	}

	evt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": reqID,
	})
	s.Events.Append(evt)
	s.Proj.Project(evt)

	fmt.Fprintf(cmd.OutOrStdout(), "Plan approved for %s. Run 'vxd resume %s' to start dispatch.\n", reqID, reqID)
	return nil
}

func newApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve <story-id>",
		Short: "Approve a story PR for merge",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runApprove,
	}
	cmd.Flags().String("all", "", "Approve all pending stories for a requirement")
	return cmd
}

func runApprove(cmd *cobra.Command, args []string) error {
	allReqID, _ := cmd.Flags().GetString("all")

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	if allReqID != "" {
		return approveAll(cmd, s, allReqID)
	}

	if len(args) == 0 {
		return fmt.Errorf("provide a story ID or use --all <req-id>")
	}

	return approveStory(cmd, s, args[0])
}

func approveStory(cmd *cobra.Command, s stores, storyID string) error {
	story, err := s.Proj.GetStory(storyID)
	if err != nil {
		return fmt.Errorf("story %s not found", storyID)
	}
	if story.Status != "awaiting_approval" {
		return fmt.Errorf("story %s is in status %q, not awaiting_approval", storyID, story.Status)
	}

	evt := state.NewEvent(state.EventStoryApproved, "human", storyID, map[string]any{
		"story_id":    storyID,
		"approved_by": "human",
	})
	s.Events.Append(evt)
	s.Proj.Project(evt)

	// Merge the existing PR
	repoDir := story.Branch // worktree path not available here; use cwd
	merger := engine.NewMerger(s.Config.Merge, nil, s.Events, s.Proj)
	if err := merger.MergeExistingPR(storyID, repoDir); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Approved: %s (PR #%d) — merge manually or re-run 'vxd resume'\n", story.Title, story.PRNumber)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Approved and merged: %s (PR #%d)\n", story.Title, story.PRNumber)
	return nil
}

func approveAll(cmd *cobra.Command, s stores, reqID string) error {
	gate := engine.NewReviewGate(s.Events)
	pending, err := gate.PendingApprovals(reqID, s.Proj)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No stories awaiting approval.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Approving %d stories...\n", len(pending))
	for _, story := range pending {
		if err := approveStory(cmd, s, story.ID); err != nil {
			return fmt.Errorf("approve %s: %w", story.ID, err)
		}
	}
	return nil
}
