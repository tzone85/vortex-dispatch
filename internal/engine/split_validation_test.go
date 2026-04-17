package engine

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestValidateSplit_RejectsDuplicateSuffixes(t *testing.T) {
	em := NewEscalationMachine(nil, config.DefaultConfig().Routing)
	children := []SplitChild{
		{Suffix: "a", Title: "First", Complexity: 2, OwnedFiles: []string{"a.go"}},
		{Suffix: "a", Title: "Duplicate", Complexity: 2, OwnedFiles: []string{"b.go"}},
	}
	err := em.ValidateSplit(0, children, 13)
	if err == nil {
		t.Fatal("expected error for duplicate suffix")
	}
}

func TestValidateSplit_AcceptsValidChildren(t *testing.T) {
	em := NewEscalationMachine(nil, config.DefaultConfig().Routing)
	children := []SplitChild{
		{Suffix: "a", Title: "First", Complexity: 2, OwnedFiles: []string{"a.go"}},
		{Suffix: "b", Title: "Second", Complexity: 2, OwnedFiles: []string{"b.go"}},
	}
	err := em.ValidateSplit(0, children, 13)
	if err != nil {
		t.Fatalf("valid children should pass: %v", err)
	}
}

func TestValidateSplitWithEdges_RejectsDanglingDepEdge(t *testing.T) {
	em := NewEscalationMachine(nil, config.DefaultConfig().Routing)
	children := []SplitChild{
		{Suffix: "a", Title: "First", Complexity: 2, OwnedFiles: []string{"a.go"}},
		{Suffix: "b", Title: "Second", Complexity: 2, OwnedFiles: []string{"b.go"}},
	}
	edges := [][]string{{"a", "nonexistent"}}
	err := em.ValidateSplitWithEdges(0, children, 13, edges)
	if err == nil {
		t.Fatal("expected error for dangling dependency edge")
	}
}

func TestValidateSplitWithEdges_RejectsMalformedEdge(t *testing.T) {
	em := NewEscalationMachine(nil, config.DefaultConfig().Routing)
	children := []SplitChild{
		{Suffix: "a", Title: "First", Complexity: 2, OwnedFiles: []string{"a.go"}},
	}
	// Edge with only one element is malformed.
	edges := [][]string{{"a"}}
	err := em.ValidateSplitWithEdges(0, children, 13, edges)
	if err == nil {
		t.Fatal("expected error for malformed edge (only 1 element)")
	}
}

func TestValidateSplitWithEdges_AcceptsValidEdges(t *testing.T) {
	em := NewEscalationMachine(nil, config.DefaultConfig().Routing)
	children := []SplitChild{
		{Suffix: "a", Title: "First", Complexity: 2, OwnedFiles: []string{"a.go"}},
		{Suffix: "b", Title: "Second", Complexity: 2, OwnedFiles: []string{"b.go"}},
	}
	// b depends on a — both suffixes exist.
	edges := [][]string{{"b", "a"}}
	err := em.ValidateSplitWithEdges(0, children, 13, edges)
	if err != nil {
		t.Fatalf("valid edges should pass: %v", err)
	}
}

func TestValidateSplitWithEdges_EmptyEdgesAllowed(t *testing.T) {
	em := NewEscalationMachine(nil, config.DefaultConfig().Routing)
	children := []SplitChild{
		{Suffix: "a", Title: "First", Complexity: 2, OwnedFiles: []string{"a.go"}},
	}
	err := em.ValidateSplitWithEdges(0, children, 13, nil)
	if err != nil {
		t.Fatalf("empty edges should pass: %v", err)
	}
}

func TestValidateSplitWithEdges_RejectsDuplicateSuffixes(t *testing.T) {
	em := NewEscalationMachine(nil, config.DefaultConfig().Routing)
	children := []SplitChild{
		{Suffix: "x", Title: "First", Complexity: 2, OwnedFiles: []string{"a.go"}},
		{Suffix: "x", Title: "Second", Complexity: 2, OwnedFiles: []string{"b.go"}},
	}
	err := em.ValidateSplitWithEdges(0, children, 13, nil)
	if err == nil {
		t.Fatal("expected error for duplicate suffix via ValidateSplitWithEdges")
	}
}
