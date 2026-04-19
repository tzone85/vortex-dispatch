package codegraph

import (
	"context"
	"testing"
)

// --- UniqueAffectedFiles coverage ---

func TestUniqueAffectedFiles_Dedup(t *testing.T) {
	ia := &ImpactAnalysis{
		ReviewPriorities: []ChangedNode{
			{Name: "F1", FilePath: "a.go", Kind: "func"},
			{Name: "F2", FilePath: "a.go", Kind: "func"},
			{Name: "F3", FilePath: "b.go", Kind: "func"},
		},
	}
	files := ia.UniqueAffectedFiles()
	if len(files) != 2 {
		t.Errorf("expected 2 unique files, got %d", len(files))
	}
}

func TestUniqueAffectedFiles_ExcludesTests(t *testing.T) {
	ia := &ImpactAnalysis{
		ReviewPriorities: []ChangedNode{
			{Name: "F1", FilePath: "a.go", Kind: "func", IsTest: false},
			{Name: "F2", FilePath: "a_test.go", Kind: "func", IsTest: true},
		},
	}
	files := ia.UniqueAffectedFiles()
	if len(files) != 1 {
		t.Errorf("expected 1 non-test file, got %d", len(files))
	}
	if files[0] != "a.go" {
		t.Errorf("expected a.go, got %q", files[0])
	}
}

func TestUniqueAffectedFiles_Sorted(t *testing.T) {
	ia := &ImpactAnalysis{
		ReviewPriorities: []ChangedNode{
			{Name: "F1", FilePath: "z.go", Kind: "func"},
			{Name: "F2", FilePath: "a.go", Kind: "func"},
		},
	}
	files := ia.UniqueAffectedFiles()
	if len(files) != 2 {
		t.Errorf("expected 2, got %d", len(files))
	}
	if files[0] != "a.go" {
		t.Errorf("expected sorted, first should be a.go, got %q", files[0])
	}
}

// --- Empty ---

func TestEmpty_Nil(t *testing.T) {
	var ia *ImpactAnalysis
	if !ia.Empty() {
		t.Error("nil should be empty")
	}
}

func TestEmpty_NoData(t *testing.T) {
	ia := &ImpactAnalysis{}
	if !ia.Empty() {
		t.Error("empty analysis should be empty")
	}
}

func TestEmpty_WithChangedFunctions(t *testing.T) {
	ia := &ImpactAnalysis{
		ChangedFunctions: []ChangedNode{{Name: "F1"}},
	}
	if ia.Empty() {
		t.Error("analysis with changed functions should not be empty")
	}
}

// --- shortPath ---

func TestShortPath_LongPath(t *testing.T) {
	got := shortPath("internal/engine/monitor.go")
	if got != "engine/monitor.go" {
		t.Errorf("expected engine/monitor.go, got %q", got)
	}
}

func TestShortPath_ShortPath(t *testing.T) {
	got := shortPath("main.go")
	if got != "main.go" {
		t.Errorf("expected main.go, got %q", got)
	}
}

func TestShortPath_TwoSegments(t *testing.T) {
	got := shortPath("pkg/util.go")
	if got != "pkg/util.go" {
		t.Errorf("expected pkg/util.go, got %q", got)
	}
}

// --- Runner Available ---

func TestRunner_Available_WithPath(t *testing.T) {
	r := &Runner{BinPath: "/usr/local/bin/code-review-graph"}
	if !r.Available() {
		t.Error("should be available with BinPath set")
	}
}

func TestRunner_Available_WithoutPath(t *testing.T) {
	r := &Runner{}
	if r.Available() {
		t.Error("should not be available without BinPath")
	}
}

// --- Runner Build available but fails ---

func TestRunner_Build_WithFakeBinary(t *testing.T) {
	r := &Runner{BinPath: "/nonexistent/binary"}
	err := r.Build(context.Background(), t.TempDir())
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}

// --- Runner Update with fake binary ---

func TestRunner_Update_WithFakeBinary(t *testing.T) {
	r := &Runner{BinPath: "/nonexistent/binary"}
	err := r.Update(context.Background(), t.TempDir(), "main")
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}

// --- Runner DetectChanges with fake binary ---

func TestRunner_DetectChanges_WithFakeBinary(t *testing.T) {
	r := &Runner{BinPath: "/nonexistent/binary"}
	_, err := r.DetectChanges(context.Background(), t.TempDir(), "main")
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}

// --- Runner Status with fake binary ---

func TestRunner_Status_WithFakeBinary(t *testing.T) {
	r := &Runner{BinPath: "/nonexistent/binary"}
	_, err := r.Status(context.Background(), t.TempDir())
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}
