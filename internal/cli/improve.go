package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func newImproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "improve",
		Short: "View self-improvement pipeline history",
		Long:  "Browse improvements discovered, implemented, and proposed by the self-improvement engine.\nUse subcommands to view the changelog, run summaries, or drill into specific findings.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), `NOTE: The self-improvement pipeline is experimental.
As of 2026-05-22, 0 actionable findings have been implemented (all 70+ findings
to date have been ecosystem news rather than VXD-actionable improvements).
Run "vxd improve runs" to see daily run history.`)
			return cmd.Help()
		},
	}

	cmd.AddCommand(newImproveLogCmd())
	cmd.AddCommand(newImproveRunsCmd())
	cmd.AddCommand(newImproveDetailCmd())
	cmd.AddCommand(newImproveAuditCmd())

	return cmd
}

// auditDirOverride lets tests inject a known audit directory without
// chdir'ing the whole process. Production code path uses CWD when this
// is empty.
var auditDirOverride string

func auditDir() string {
	if auditDirOverride != "" {
		return auditDirOverride
	}
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "docs", "self-improvement")
}

// --- vxd improve log ---

func newImproveLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show improvement changelog with filtering",
		RunE:  runImproveLog,
	}
	cmd.Flags().String("disposition", "", "Filter: implemented, proposed, aborted")
	cmd.Flags().String("category", "", "Filter by category (security, competitors, go_ecosystem, llm_providers)")
	cmd.Flags().String("since", "", "Show entries since date (YYYY-MM-DD)")
	cmd.Flags().Int("limit", 25, "Max entries to show")
	cmd.Flags().Bool("errors", false, "Show only entries with errors")
	cmd.Flags().Bool("json", false, "Output raw JSONL")
	cmd.SilenceUsage = true
	return cmd
}

func runImproveLog(cmd *cobra.Command, _ []string) error {
	disposition, _ := cmd.Flags().GetString("disposition")
	category, _ := cmd.Flags().GetString("category")
	sinceStr, _ := cmd.Flags().GetString("since")
	limit, _ := cmd.Flags().GetInt("limit")
	errorsOnly, _ := cmd.Flags().GetBool("errors")
	jsonOut, _ := cmd.Flags().GetBool("json")

	log := improve.NewAuditLog(auditDir())

	var entries []improve.AuditEntry
	var err error

	if sinceStr != "" {
		since, parseErr := time.Parse("2006-01-02", sinceStr)
		if parseErr != nil {
			return fmt.Errorf("invalid date %q (use YYYY-MM-DD): %w", sinceStr, parseErr)
		}
		entries, err = log.ReadSince(since)
	} else {
		entries, err = log.ReadAll()
	}
	if err != nil {
		return fmt.Errorf("read audit log: %w", err)
	}

	// Apply filters
	filtered := make([]improve.AuditEntry, 0, len(entries))
	for _, e := range entries {
		if disposition != "" && e.Disposition != disposition {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		if errorsOnly && e.Error == "" {
			continue
		}
		filtered = append(filtered, e)
	}

	if jsonOut {
		for _, e := range filtered {
			fmt.Printf("%s\n", mustJSON(e))
		}
		return nil
	}

	// Show most recent first
	reversed := reverseEntries(filtered)
	if len(reversed) > limit {
		reversed = reversed[:limit]
	}

	if len(reversed) == 0 {
		fmt.Println("No improvement entries found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tDISPOSITION\tCATEGORY\tTITLE\tPR")
	fmt.Fprintln(w, "----\t-----------\t--------\t-----\t--")
	for _, e := range reversed {
		date := extractDate(e.RunID)
		title := e.Title
		if len(title) > 45 {
			title = title[:42] + "..."
		}
		pr := "-"
		if e.PRURL != "" {
			pr = e.PRURL
		}
		disp := dispositionIcon(e.Disposition) + " " + e.Disposition
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", date, disp, e.Category, title, pr)
	}
	w.Flush()

	// Summary stats
	stats := computeStats(filtered)
	fmt.Printf("\nTotal: %d | Implemented: %d | Proposed: %d | Aborted: %d",
		stats.total, stats.implemented, stats.proposed, stats.aborted)
	if stats.withErrors > 0 {
		fmt.Printf(" | Errors: %d", stats.withErrors)
	}
	fmt.Println()

	return nil
}

// --- vxd improve runs ---

func newImproveRunsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Show daily run summaries",
		RunE:  runImproveRuns,
	}
	cmd.Flags().Int("limit", 14, "Max runs to show")
	cmd.SilenceUsage = true
	return cmd
}

func runImproveRuns(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	runsDir := filepath.Join(auditDir(), "runs")

	dirEntries, err := os.ReadDir(runsDir)
	if err != nil {
		return fmt.Errorf("read runs dir: %w", err)
	}

	// Collect run files (most recent first)
	type runFile struct {
		date    string
		summary improve.RunSummary
	}
	var runs []runFile
	for i := len(dirEntries) - 1; i >= 0; i-- {
		entry := dirEntries[i]
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ".json")
		summary, loadErr := improve.LoadRunSummary(runsDir, date)
		if loadErr != nil {
			continue
		}
		runs = append(runs, runFile{date: date, summary: summary})
		if len(runs) >= limit {
			break
		}
	}

	if len(runs) == 0 {
		fmt.Println("No run summaries found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tFINDINGS\tRELEVANT\tPRs\tEMAIL\tDURATION\tERRORS")
	fmt.Fprintln(w, "----\t--------\t--------\t---\t-----\t--------\t------")
	for _, r := range runs {
		s := r.summary
		duration := s.CompletedAt.Sub(s.StartedAt).Round(time.Second)
		email := "no"
		if s.EmailSent {
			email = "yes"
		}
		errCount := len(s.Errors)
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\t%s\t%d\n",
			r.date, s.FindingsTotal, s.FindingsRelevant, s.PRsCreated,
			email, duration, errCount)
	}
	w.Flush()

	return nil
}

// --- vxd improve detail ---

func newImproveDetailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detail <finding-id>",
		Short: "Show full details of a specific finding",
		Long:  "Show complete information about a finding including reasoning, errors, and PR links.\nUse 'vxd improve log' to find finding IDs (e.g. f-2026-04-18-001).",
		Args:  cobra.ExactArgs(1),
		RunE:  runImproveDetail,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runImproveDetail(_ *cobra.Command, args []string) error {
	findingID := args[0]

	log := improve.NewAuditLog(auditDir())
	entries, err := log.ReadAll()
	if err != nil {
		return fmt.Errorf("read audit log: %w", err)
	}

	var found *improve.AuditEntry
	for i, e := range entries {
		if e.FindingID == findingID {
			found = &entries[i]
			break
		}
	}

	if found == nil {
		return fmt.Errorf("finding %q not found in changelog", findingID)
	}

	e := found
	fmt.Printf("Finding: %s\n", e.FindingID)
	fmt.Printf("Title:   %s\n", e.Title)
	fmt.Printf("Date:    %s\n", extractDate(e.RunID))
	fmt.Printf("Source:  %s\n", e.Source)
	fmt.Printf("Category: %s\n", e.Category)
	fmt.Println()
	fmt.Printf("Disposition: %s %s\n", dispositionIcon(e.Disposition), e.Disposition)
	fmt.Printf("Relevance: %d  Impact: %d  Risk: %d\n", e.Relevance, e.Impact, e.Risk)
	if e.PRURL != "" {
		fmt.Printf("PR: %s\n", e.PRURL)
	}
	if e.TestsPassed {
		fmt.Println("Tests: PASSED")
	}
	if e.FilesChanged > 0 {
		fmt.Printf("Changes: %d files, %d lines\n", e.FilesChanged, e.LinesChanged)
	}
	if e.Error != "" {
		fmt.Printf("\nError: %s\n", e.Error)
	}
	fmt.Printf("\nReasoning:\n%s\n", e.Reasoning)
	if e.SecurityReview != "" {
		fmt.Printf("\nSecurity: %s\n", e.SecurityReview)
	}

	return nil
}

// --- vxd improve audit ---

func newImproveAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Run the self-audit (build, vet, gofmt, test, govulncheck, go mod tidy) on this repo",
		Long: "Run the repository's own toolchain and report real findings.\n" +
			"Deterministic findings (gofmt, go mod tidy) can be fixed in the working tree with --fix.\n" +
			"Judgement findings (build, vet, test, govulncheck) are only ever reported.\n" +
			"This command never writes to the improvement changelog.",
		RunE: runImproveAudit,
	}
	cmd.Flags().String("repo", "", "Repository root (default: current directory)")
	cmd.Flags().Bool("json", false, "Output the report as JSON")
	cmd.Flags().Bool("skip-tests", false, "Skip the (slow) go test check")
	cmd.Flags().String("timeout", "15m", "Value for go test -timeout")
	cmd.Flags().Bool("fix", false, "Apply deterministic fixes (gofmt, go mod tidy) in the working tree; no branch or PR")
	cmd.SilenceUsage = true
	return cmd
}

func runImproveAudit(cmd *cobra.Command, _ []string) error {
	repo, _ := cmd.Flags().GetString("repo")
	jsonOut, _ := cmd.Flags().GetBool("json")
	skipTests, _ := cmd.Flags().GetBool("skip-tests")
	timeout, _ := cmd.Flags().GetString("timeout")
	fix, _ := cmd.Flags().GetBool("fix")

	if repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determine working directory: %w", err)
		}
		repo = cwd
	}
	if info, err := os.Stat(repo); err != nil || !info.IsDir() {
		return fmt.Errorf("self-audit: repo %q is not a directory", repo)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer cancel()

	auditor := improve.NewSelfAuditor(repo, nil)
	auditor.TestTimeout = timeout
	auditor.SkipTests = skipTests
	rep, err := auditor.Audit(ctx)
	if err != nil {
		return fmt.Errorf("self-audit: %w", err)
	}

	out := cmd.OutOrStdout()
	if jsonOut {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal report: %w", err)
		}
		fmt.Fprintln(out, string(data))
	} else {
		fmt.Fprint(out, rep.Markdown())
	}

	if skipped := rep.Skipped(); len(skipped) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "\nWARNING: %d check(s) did not run; this is not a clean bill of health.\n", len(skipped))
		if len(skipped) == len(rep.Results) {
			return fmt.Errorf("self-audit: no check could run (is the Go toolchain on PATH and %q a Go module?)", repo)
		}
	}

	remaining := len(rep.Findings)
	if fix {
		det := rep.DeterministicFindings()
		applied, fixErr := improve.ApplyDeterministicFixes(ctx, improve.ExecRunner{}, repo, det)
		for _, a := range applied {
			fmt.Fprintf(cmd.ErrOrStderr(), "fixed: %s\n", a)
		}
		if fixErr != nil {
			return fmt.Errorf("apply fixes: %w", fixErr)
		}
		remaining -= len(det)
	}

	if remaining > 0 {
		return fmt.Errorf("self-audit: %d finding(s) need attention", remaining)
	}
	return nil
}

// --- helpers ---

func dispositionIcon(d string) string {
	switch d {
	case "implemented":
		return "[OK]"
	case "proposed":
		return "[--]"
	case "aborted":
		return "[!!]"
	default:
		return "[??]"
	}
}

func extractDate(runID string) string {
	t, err := time.Parse(time.RFC3339, runID)
	if err != nil {
		return runID[:10]
	}
	return t.Format("2006-01-02")
}

type logStats struct {
	total, implemented, proposed, aborted, withErrors int
}

func computeStats(entries []improve.AuditEntry) logStats {
	var s logStats
	s.total = len(entries)
	for _, e := range entries {
		switch e.Disposition {
		case "implemented":
			s.implemented++
		case "proposed":
			s.proposed++
		case "aborted":
			s.aborted++
		}
		if e.Error != "" {
			s.withErrors++
		}
	}
	return s
}

func reverseEntries(entries []improve.AuditEntry) []improve.AuditEntry {
	out := make([]improve.AuditEntry, len(entries))
	for i, e := range entries {
		out[len(entries)-1-i] = e
	}
	return out
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
