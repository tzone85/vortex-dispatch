package codegraph

import (
	"context"
	"strings"
	"testing"
)

// --- GraphDB Close nil db ---

func TestGraphDB_Close_Nil(t *testing.T) {
	g := &GraphDB{db: nil}
	err := g.Close()
	if err != nil {
		t.Errorf("closing nil db should not error, got %v", err)
	}
}

// --- Runner Update with base ref ---

func TestRunner_Update_WithBaseRef(t *testing.T) {
	r := &Runner{BinPath: ""}
	err := r.Update(context.Background(), "/tmp", "main")
	if err == nil {
		t.Error("expected error for unavailable runner")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected not installed error, got: %v", err)
	}
}

// --- Runner DetectChanges with base ref ---

func TestRunner_DetectChanges_WithBase(t *testing.T) {
	r := &Runner{BinPath: ""}
	ia, err := r.DetectChanges(context.Background(), "/tmp", "main")
	if err != nil {
		t.Errorf("unavailable runner should return empty analysis, got error: %v", err)
	}
	if ia == nil {
		t.Error("expected non-nil ImpactAnalysis")
	}
}

// --- parseDetectChanges edge cases ---

func TestParseDetectChanges_EmptyJSON(t *testing.T) {
	ia, err := parseDetectChanges("{}")
	if err != nil {
		t.Fatal(err)
	}
	if ia.RiskScore != 0 {
		t.Errorf("expected 0 risk score, got %f", ia.RiskScore)
	}
}

func TestParseDetectChanges_InvalidJSON(t *testing.T) {
	_, err := parseDetectChanges("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseDetectChanges_WithAllFields(t *testing.T) {
	input := `{
		"summary": "High risk changes",
		"risk_score": 8.5,
		"changed_functions": [
			{"name": "Func1", "file_path": "main.go", "kind": "function", "line_start": 10, "line_end": 20, "is_test": false, "risk_score": 7.0}
		],
		"test_gaps": [
			{"name": "Func2", "file": "util.go", "line_start": 30, "line_end": 40}
		],
		"review_priorities": [
			{"name": "Func3", "file_path": "core.go", "kind": "method", "line_start": 50, "line_end": 60, "is_test": true, "risk_score": 9.0}
		]
	}`
	ia, err := parseDetectChanges(input)
	if err != nil {
		t.Fatal(err)
	}
	if ia.RiskScore != 8.5 {
		t.Errorf("expected risk 8.5, got %f", ia.RiskScore)
	}
	if len(ia.ChangedFunctions) != 1 {
		t.Errorf("expected 1 changed function, got %d", len(ia.ChangedFunctions))
	}
	if len(ia.TestGaps) != 1 {
		t.Errorf("expected 1 test gap, got %d", len(ia.TestGaps))
	}
	if len(ia.ReviewPriorities) != 1 {
		t.Errorf("expected 1 review priority, got %d", len(ia.ReviewPriorities))
	}
	if ia.Summary != "High risk changes" {
		t.Errorf("unexpected summary: %s", ia.Summary)
	}
}

// --- parseStatus edge cases ---

func TestParseStatus_EmptyInput(t *testing.T) {
	info, err := parseStatus("")
	if err != nil {
		t.Fatal(err)
	}
	if info.NodeCount != 0 {
		t.Errorf("expected 0 nodes, got %d", info.NodeCount)
	}
}

func TestParseStatus_PartialInput(t *testing.T) {
	input := "Nodes: 100\nEdges: 500\n"
	info, err := parseStatus(input)
	if err != nil {
		t.Fatal(err)
	}
	if info.NodeCount != 100 {
		t.Errorf("expected 100 nodes, got %d", info.NodeCount)
	}
	if info.EdgeCount != 500 {
		t.Errorf("expected 500 edges, got %d", info.EdgeCount)
	}
}

func TestParseStatus_WithLanguagesAndCommit(t *testing.T) {
	input := "Nodes: 50\nEdges: 200\nFiles: 30\nLanguages: Go, Python, JavaScript\nLast updated: 2025-01-01T00:00:00Z\nBuilt at commit: abc123\n"
	info, err := parseStatus(input)
	if err != nil {
		t.Fatal(err)
	}
	if info.FileCount != 30 {
		t.Errorf("expected 30 files, got %d", info.FileCount)
	}
	if len(info.Languages) != 3 {
		t.Errorf("expected 3 languages, got %d", len(info.Languages))
	}
	if info.CommitHash != "abc123" {
		t.Errorf("expected abc123 commit, got %q", info.CommitHash)
	}
}

// --- Runner Status unavailable ---

func TestRunner_Status_Unavailable(t *testing.T) {
	r := &Runner{BinPath: ""}
	info, err := r.Status(context.Background(), "/tmp")
	if err != nil {
		t.Errorf("unavailable runner should return empty info, got error: %v", err)
	}
	if info.NodeCount != 0 {
		t.Errorf("expected 0 nodes, got %d", info.NodeCount)
	}
}

// --- NewRunner ---

func TestNewRunner_SetsPath(t *testing.T) {
	r := NewRunner()
	// We just verify it doesn't panic; BinPath may or may not be set
	// depending on whether code-review-graph is installed
	if r == nil {
		t.Error("expected non-nil runner")
	}
}

// --- FormatMarkdown coverage ---

func TestFormatMarkdown_WithTestGaps(t *testing.T) {
	ia := &ImpactAnalysis{
		RiskScore: 5.0,
		Summary:   "Medium risk",
		ChangedFunctions: []ChangedNode{
			{Name: "Func1", FilePath: "a.go", Kind: "func", LineStart: 1, LineEnd: 10},
		},
		TestGaps: []TestGap{
			{Name: "Func2", FilePath: "b.go", LineStart: 20, LineEnd: 30},
		},
		ReviewPriorities: []ChangedNode{
			{Name: "Func3", FilePath: "c.go", Kind: "method", LineStart: 40, LineEnd: 50, RiskScore: 8.0},
		},
		AffectedFiles: []string{"a.go", "b.go", "c.go"},
	}
	md := ia.FormatMarkdown()
	if !strings.Contains(md, "Test Gaps") {
		t.Error("expected Test Gaps section")
	}
	if !strings.Contains(md, "Review Priorities") {
		t.Error("expected Review Priorities section")
	}
	if !strings.Contains(md, "Blast Radius") {
		t.Error("expected Blast Radius header")
	}
}

// --- GraphDBPath ---

func TestGraphDBPath_Format(t *testing.T) {
	got := GraphDBPath("/my/repo")
	expected := "/my/repo/.code-review-graph/graph.db"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}
