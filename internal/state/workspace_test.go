package state_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestSQLiteStore_ListRequirementsFiltered_ByRepoPath(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	// Submit requirements from two different repos
	for _, tc := range []struct {
		id       string
		title    string
		repoPath string
	}{
		{"r-001", "Feature A", "/home/user/repo-alpha"},
		{"r-002", "Feature B", "/home/user/repo-alpha"},
		{"r-003", "Feature C", "/home/user/repo-beta"},
	} {
		evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
			"id":          tc.id,
			"title":       tc.title,
			"description": tc.title,
			"repo_path":   tc.repoPath,
		})
		if err := db.Project(evt); err != nil {
			t.Fatalf("project req %s: %v", tc.id, err)
		}
	}

	// Filter by repo-alpha should return 2 requirements
	alphaReqs, err := db.ListRequirementsFiltered(state.ReqFilter{RepoPath: "/home/user/repo-alpha"})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(alphaReqs) != 2 {
		t.Fatalf("expected 2 requirements for repo-alpha, got %d", len(alphaReqs))
	}

	// Filter by repo-beta should return 1 requirement
	betaReqs, err := db.ListRequirementsFiltered(state.ReqFilter{RepoPath: "/home/user/repo-beta"})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(betaReqs) != 1 {
		t.Fatalf("expected 1 requirement for repo-beta, got %d", len(betaReqs))
	}
	if betaReqs[0].ID != "r-003" {
		t.Fatalf("expected r-003, got %s", betaReqs[0].ID)
	}

	// No filter should return all 3
	allReqs, err := db.ListRequirementsFiltered(state.ReqFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(allReqs) != 3 {
		t.Fatalf("expected 3 requirements total, got %d", len(allReqs))
	}
}

func TestSQLiteStore_ListRequirementsFiltered_ExcludeArchived(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	// Submit two requirements
	for _, id := range []string{"r-001", "r-002"} {
		evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
			"id":          id,
			"title":       "Req " + id,
			"description": "Desc " + id,
			"repo_path":   "/home/user/repo",
		})
		if err := db.Project(evt); err != nil {
			t.Fatalf("project req %s: %v", id, err)
		}
	}

	// Archive one
	if err := db.ArchiveRequirement("r-001"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// ExcludeArchived should return only the non-archived one
	filtered, err := db.ListRequirementsFiltered(state.ReqFilter{ExcludeArchived: true})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 non-archived requirement, got %d", len(filtered))
	}
	if filtered[0].ID != "r-002" {
		t.Fatalf("expected r-002, got %s", filtered[0].ID)
	}

	// Without ExcludeArchived, both should appear
	all, err := db.ListRequirementsFiltered(state.ReqFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 requirements total, got %d", len(all))
	}
}

func TestSQLiteStore_ListRequirementsFiltered_CombinedFilters(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	// Submit requirements: 2 in repo-alpha, 1 in repo-beta
	for _, tc := range []struct {
		id       string
		repoPath string
	}{
		{"r-001", "/home/user/repo-alpha"},
		{"r-002", "/home/user/repo-alpha"},
		{"r-003", "/home/user/repo-beta"},
	} {
		evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
			"id":          tc.id,
			"title":       "Req " + tc.id,
			"description": "Desc",
			"repo_path":   tc.repoPath,
		})
		if err := db.Project(evt); err != nil {
			t.Fatalf("project req %s: %v", tc.id, err)
		}
	}

	// Archive r-001 (in repo-alpha)
	if err := db.ArchiveRequirement("r-001"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Filter: repo-alpha, exclude archived -> should get only r-002
	filtered, err := db.ListRequirementsFiltered(state.ReqFilter{
		RepoPath:        "/home/user/repo-alpha",
		ExcludeArchived: true,
	})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(filtered))
	}
	if filtered[0].ID != "r-002" {
		t.Fatalf("expected r-002, got %s", filtered[0].ID)
	}
}

func TestSQLiteStore_RequirementRepoPath(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "r-rp-001",
		"title":       "Test Req",
		"description": "Testing repo path",
		"repo_path":   "/home/user/my-project",
	})
	if err := db.Project(evt); err != nil {
		t.Fatalf("project: %v", err)
	}

	req, err := db.GetRequirement("r-rp-001")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.RepoPath != "/home/user/my-project" {
		t.Fatalf("expected repo_path '/home/user/my-project', got '%s'", req.RepoPath)
	}
}
