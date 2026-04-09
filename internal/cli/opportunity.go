package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func newOpportunityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "opportunity",
		Aliases: []string{"opp"},
		Short:   "Manage the opportunity pipeline",
		Long:    "View, filter, and manage freelance/contract opportunities discovered by the revenue engine.",
	}

	cmd.AddCommand(newOppListCmd())
	cmd.AddCommand(newOppProposeCmd())
	cmd.AddCommand(newOppStatusCmd())
	cmd.AddCommand(newOppWonCmd())
	cmd.AddCommand(newOppSourcesCmd())
	cmd.AddCommand(newOppApproveSourceCmd())

	return cmd
}

func opportunitiesDir() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "docs", "opportunities")
}

func pipelinePath() string {
	return filepath.Join(opportunitiesDir(), "pipeline.jsonl")
}

func newOppListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show opportunity pipeline sorted by rank",
		RunE:  runOppList,
	}
	cmd.Flags().String("status", "", "Filter by status (new, interested, proposal_drafted, sent, won, lost, expired)")
	cmd.Flags().Int("limit", 20, "Max opportunities to show")
	cmd.SilenceUsage = true
	return cmd
}

func runOppList(cmd *cobra.Command, _ []string) error {
	status, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")

	opps, err := improve.ReadOpportunities(pipelinePath())
	if err != nil {
		return fmt.Errorf("read pipeline: %w", err)
	}

	if status != "" {
		opps = improve.FilterByStatus(opps, status)
	}

	opps = improve.SortByRank(opps)
	if len(opps) > limit {
		opps = opps[:limit]
	}

	if len(opps) == 0 {
		fmt.Println("No opportunities in pipeline.")
		fmt.Println("Run vxd-improve to scan for opportunities.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RANK\tID\tSOURCE\tTITLE\tBUDGET\tSTATUS")
	fmt.Fprintln(w, "----\t--\t------\t-----\t------\t------")
	for _, opp := range opps {
		title := opp.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			opp.Rank, opp.ID, opp.Source, title, opp.Budget, opp.Status)
	}
	w.Flush()

	// Summary
	allOpps, _ := improve.ReadOpportunities(pipelinePath())
	revPath := filepath.Join(opportunitiesDir(), "revenue.jsonl")
	entries, _ := improve.ReadRevenue(revPath)
	total := improve.TotalRevenue(entries)
	fmt.Printf("\nPipeline: %d total | Revenue: $%.0f\n", len(allOpps), total)

	return nil
}

func newOppProposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "propose <id>",
		Short: "Draft a proposal for a specific opportunity",
		Args:  cobra.ExactArgs(1),
		RunE:  runOppPropose,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runOppPropose(cmd *cobra.Command, args []string) error {
	id := args[0]

	opps, err := improve.ReadOpportunities(pipelinePath())
	if err != nil {
		return fmt.Errorf("read pipeline: %w", err)
	}

	var target *improve.Opportunity
	for i, opp := range opps {
		if opp.ID == id {
			target = &opps[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("opportunity %q not found", id)
	}

	claudePath := "claude"
	if cp := os.Getenv("CLAUDE_PATH"); cp != "" {
		claudePath = cp
	}

	proposalDir := filepath.Join(opportunitiesDir(), "proposals")
	drafter := improve.NewProposalDrafter(claudePath, proposalDir)

	ctx := cmd.Context()
	draft, err := drafter.DraftProposal(ctx, *target)
	if err != nil {
		return fmt.Errorf("draft proposal: %w", err)
	}

	fmt.Println(draft)

	// Update pipeline
	improve.UpdateOpportunityField(pipelinePath(), id, func(opp improve.Opportunity) improve.Opportunity {
		opp.ProposalDraft = draft
		opp.Status = improve.StatusProposalDrafted
		return opp
	})

	return nil
}

func newOppStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <id> <new-status>",
		Short: "Update opportunity status",
		Args:  cobra.ExactArgs(2),
		RunE:  runOppStatus,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runOppStatus(_ *cobra.Command, args []string) error {
	id := args[0]
	newStatus := args[1]

	validStatuses := map[string]bool{
		"new": true, "reviewed": true, "interested": true,
		"proposal_drafted": true, "sent": true,
		"won": true, "lost": true, "expired": true,
	}
	if !validStatuses[newStatus] {
		return fmt.Errorf("invalid status %q. Valid: new, reviewed, interested, proposal_drafted, sent, won, lost, expired", newStatus)
	}

	// Read the opportunity before updating to capture metadata for feedback
	opps, _ := improve.ReadOpportunities(pipelinePath())
	var matchedOpp *improve.Opportunity
	for i, opp := range opps {
		if opp.ID == id {
			matchedOpp = &opps[i]
			break
		}
	}

	updated, err := improve.UpdateOpportunityStatus(pipelinePath(), id, newStatus)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	// Log feedback for terminal outcomes
	if newStatus == "lost" || newStatus == "expired" {
		feedbackPath := filepath.Join(opportunitiesDir(), "feedback.jsonl")
		fl := improve.NewFeedbackLoop(feedbackPath)
		fbEntry := improve.FeedbackEntry{
			Type:      "proposal",
			Outcome:   newStatus,
			Timestamp: time.Now(),
		}
		if matchedOpp != nil {
			fbEntry.Source = matchedOpp.Source
			fbEntry.SkillSet = strings.Join(matchedOpp.Skills, ",")
			fbEntry.PriceRange = matchedOpp.Budget
		}
		if fbErr := fl.AppendFeedback(fbEntry); fbErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to log feedback: %v\n", fbErr)
		}
	}

	fmt.Printf("Updated %s: %s -> %s\n", updated.ID, id, newStatus)
	return nil
}

func newOppWonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "won <id> <amount>",
		Short: "Log revenue for a won opportunity",
		Args:  cobra.ExactArgs(2),
		RunE:  runOppWon,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runOppWon(_ *cobra.Command, args []string) error {
	id := args[0]

	var amount float64
	if _, err := fmt.Sscanf(args[1], "%f", &amount); err != nil {
		return fmt.Errorf("invalid amount %q: %w", args[1], err)
	}

	// Read the opportunity to capture metadata for feedback
	opps, _ := improve.ReadOpportunities(pipelinePath())
	var matchedOpp *improve.Opportunity
	for i, opp := range opps {
		if opp.ID == id {
			matchedOpp = &opps[i]
			break
		}
	}

	// Update status to won
	_, err := improve.UpdateOpportunityStatus(pipelinePath(), id, improve.StatusWon)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	// Read existing revenue
	revPath := filepath.Join(opportunitiesDir(), "revenue.jsonl")
	entries, _ := improve.ReadRevenue(revPath)
	existingTotal := improve.TotalRevenue(entries)
	newTotal := existingTotal + amount

	// Append revenue entry
	entry := improve.RevenueEntry{
		OpportunityID:   id,
		Amount:          amount,
		Currency:        "USD",
		Date:            time.Now().Format("2006-01-02"),
		Status:          "received",
		CumulativeTotal: newTotal,
	}
	if err := improve.AppendRevenue(revPath, entry); err != nil {
		return fmt.Errorf("append revenue: %w", err)
	}

	// Log feedback entry for Bayesian learning
	feedbackPath := filepath.Join(opportunitiesDir(), "feedback.jsonl")
	fl := improve.NewFeedbackLoop(feedbackPath)
	fbEntry := improve.FeedbackEntry{
		Type:      "proposal",
		Outcome:   "won",
		Timestamp: time.Now(),
	}
	if matchedOpp != nil {
		fbEntry.Source = matchedOpp.Source
		fbEntry.SkillSet = strings.Join(matchedOpp.Skills, ",")
		fbEntry.PriceRange = matchedOpp.Budget
	}
	if fbErr := fl.AppendFeedback(fbEntry); fbErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log feedback: %v\n", fbErr)
	}

	fmt.Printf("Logged $%.0f revenue for %s\n", amount, id)
	fmt.Printf("Cumulative total: $%.0f\n", newTotal)

	// Check milestone
	milestone := improve.CheckMilestone(newTotal)
	if milestone > 0 && improve.CheckMilestone(existingTotal) < milestone {
		fmt.Printf("\nMission Milestone: $%.0f reached!\n", milestone)
		fmt.Println("You started this to free your village from poverty. Keep going!")
	}

	return nil
}

func newOppSourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "Show discovered sources pending approval",
		RunE:  runOppSources,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runOppSources(_ *cobra.Command, _ []string) error {
	sourcesPath := filepath.Join(opportunitiesDir(), "discovered_sources.jsonl")
	sources, err := improve.ReadDiscoveredSources(sourcesPath)
	if err != nil {
		return fmt.Errorf("read sources: %w", err)
	}

	if len(sources) == 0 {
		fmt.Println("No discovered sources yet.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tNAME\tURL\tDISCOVERED\tREASON")
	fmt.Fprintln(w, "------\t----\t---\t----------\t------")
	for _, s := range sources {
		reason := s.Reason
		if len(reason) > 50 {
			reason = reason[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.Status, s.Name, s.URL, s.DiscoveredOn, reason)
	}
	w.Flush()
	return nil
}

func newOppApproveSourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve-source <url>",
		Short: "Approve a discovered source for active scraping",
		Args:  cobra.ExactArgs(1),
		RunE:  runOppApproveSource,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runOppApproveSource(_ *cobra.Command, args []string) error {
	url := args[0]
	sourcesPath := filepath.Join(opportunitiesDir(), "discovered_sources.jsonl")

	if err := improve.ApproveSource(sourcesPath, url); err != nil {
		return fmt.Errorf("approve source: %w", err)
	}

	fmt.Printf("Approved source: %s\n", url)
	return nil
}
