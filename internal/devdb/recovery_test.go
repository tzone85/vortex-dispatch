package devdb_test

import (
	"context"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/null"
)

// listProvider lets tests inject a fixed List() result and capture Delete calls.
type listProvider struct {
	*null.Provider
	dbs     []devdb.DB
	deleted []string
}

func (p *listProvider) List(ctx context.Context) ([]devdb.DB, error) { return p.dbs, nil }
func (p *listProvider) Delete(ctx context.Context, id string) error {
	p.deleted = append(p.deleted, id)
	return nil
}

func TestFindOrphans_FiltersByPrefixAndActiveSet(t *testing.T) {
	now := time.Now()
	p := &listProvider{
		Provider: null.New(),
		dbs: []devdb.DB{
			{ID: "1", Name: "vxd-myproj-active-story", CreatedAt: now.Add(-2 * time.Hour)},
			{ID: "2", Name: "vxd-myproj-orphan-story", CreatedAt: now.Add(-2 * time.Hour)},
			{ID: "3", Name: "other-prefix-something", CreatedAt: now.Add(-2 * time.Hour)},
		},
	}
	active := []string{"active-story"}
	got, err := devdb.FindOrphans(context.Background(), p, "vxd", active)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "2" {
		t.Errorf("orphans = %+v, want [{ID:2 ...}]", got)
	}
}

func TestReleaseOrphans_HonorsMinAge(t *testing.T) {
	now := time.Now()
	p := &listProvider{
		Provider: null.New(),
		dbs: []devdb.DB{
			{ID: "old", Name: "vxd-x-old-1", CreatedAt: now.Add(-25 * time.Hour)},
			{ID: "new", Name: "vxd-x-new-1", CreatedAt: now.Add(-1 * time.Hour)},
		},
	}
	deleted, kept, err := devdb.ReleaseOrphans(context.Background(), p, p.dbs, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].ID != "old" {
		t.Errorf("deleted = %+v, want [old]", deleted)
	}
	if len(kept) != 1 || kept[0].ID != "new" {
		t.Errorf("kept = %+v, want [new]", kept)
	}
	if len(p.deleted) != 1 || p.deleted[0] != "old" {
		t.Errorf("provider delete calls = %v", p.deleted)
	}
}
