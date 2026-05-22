package ghost_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/ghost"
)

// stubServer returns an httptest.Server that handles standard Ghost endpoints.
// The caller supplies a handler map: path → handler function. Unmatched paths
// get 404.
func stubServer(t *testing.T, routes map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for pattern, h := range routes {
			if r.URL.Path == pattern {
				h(w, r)
				return
			}
		}
		t.Logf("stub: unmatched %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
}

// spacesBody returns a JSON body with one space for lazy resolution.
func spacesBody(spaceID string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"spaces": []map[string]string{{"id": spaceID}},
	})
	return b
}

// dbsBody returns a JSON body wrapping a databases array.
func dbsBody(dbs []map[string]string) []byte {
	b, _ := json.Marshal(map[string]interface{}{"databases": dbs})
	return b
}

func newProvider(t *testing.T, baseURL string) *ghost.Provider {
	t.Helper()
	p, err := ghost.New(ghost.Config{APIKey: "test-key", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("ghost.New: %v", err)
	}
	return p
}

// -------- Construction --------

func TestProvider_Name(t *testing.T) {
	p, err := ghost.New(ghost.Config{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "ghost" {
		t.Errorf("Name = %q, want ghost", p.Name())
	}
}

func TestProvider_SatisfiesInterface(t *testing.T) {
	p, _ := ghost.New(ghost.Config{APIKey: "test"})
	var _ devdb.Provider = p
}

func TestProvider_New_RequiresAPIKey(t *testing.T) {
	if _, err := ghost.New(ghost.Config{}); err == nil {
		t.Error("expected error when APIKey is empty")
	}
}

// -------- Ping --------

func TestProvider_Ping_Healthy(t *testing.T) {
	srv := stubServer(t, map[string]http.HandlerFunc{
		"/health": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
	})
	defer srv.Close()

	p := newProvider(t, srv.URL)
	if err := p.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestProvider_Ping_Unreachable(t *testing.T) {
	srv := stubServer(t, map[string]http.HandlerFunc{
		"/health": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) },
	})
	defer srv.Close()

	p := newProvider(t, srv.URL)
	err := p.Ping(context.Background())
	if !errors.Is(err, devdb.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown, got: %v", err)
	}
}

// -------- Create --------

func TestProvider_Create_HappyPath(t *testing.T) {
	const spaceID = "sp-1"
	srv := stubServer(t, map[string]http.HandlerFunc{
		"/spaces": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		},
		"/spaces/" + spaceID + "/databases": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": "db-1", "name": "vxd-proj-a1b2c3d4-z1", "status": "running",
				"dsn": "postgres://u:p@h/db",
			})
		},
	})
	defer srv.Close()

	p := newProvider(t, srv.URL)
	db, err := p.Create(context.Background(), devdb.CreateOpts{Name: "vxd-proj-a1b2c3d4-z1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if db.Provider != "ghost" {
		t.Errorf("Provider = %q, want ghost", db.Provider)
	}
	if db.ID != "db-1" {
		t.Errorf("ID = %q, want db-1", db.ID)
	}
	if db.ConnectionString != "postgres://u:p@h/db" {
		t.Errorf("ConnectionString = %q", db.ConnectionString)
	}
}

func TestProvider_Create_InvalidName(t *testing.T) {
	p, _ := ghost.New(ghost.Config{APIKey: "test"})
	_, err := p.Create(context.Background(), devdb.CreateOpts{Name: "INVALID NAME!"})
	if !errors.Is(err, devdb.ErrInvalidName) {
		t.Errorf("expected ErrInvalidName, got: %v", err)
	}
}

// -------- Fork --------

func TestProvider_Fork_HappyPath(t *testing.T) {
	const (
		spaceID  = "sp-2"
		tplName  = "my-template"
		tplID    = "tpl-id-abc"
		forkName = "vxd-fork-a1b2c3d4-z1"
	)
	srv := stubServer(t, map[string]http.HandlerFunc{
		"/spaces": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		},
		"/spaces/" + spaceID + "/databases": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(dbsBody([]map[string]string{
				{"id": tplID, "name": tplName, "status": "running", "dsn": "postgres://t"},
			}))
		},
		"/spaces/" + spaceID + "/databases/" + tplID + "/fork": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": "fork-id-1", "name": forkName, "status": "running",
				"dsn": "postgres://u:p@h/fork",
			})
		},
	})
	defer srv.Close()

	p := newProvider(t, srv.URL)
	db, err := p.Fork(context.Background(), tplName, devdb.CreateOpts{Name: forkName})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if db.ID != "fork-id-1" {
		t.Errorf("ID = %q, want fork-id-1", db.ID)
	}
}

func TestProvider_Fork_TemplateNotFound(t *testing.T) {
	const spaceID = "sp-3"
	srv := stubServer(t, map[string]http.HandlerFunc{
		"/spaces": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		},
		"/spaces/" + spaceID + "/databases": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(dbsBody(nil))
		},
	})
	defer srv.Close()

	p := newProvider(t, srv.URL)
	_, err := p.Fork(context.Background(), "missing-template", devdb.CreateOpts{Name: "vxd-proj-a1b2c3d4-z1"})
	if !errors.Is(err, devdb.ErrTemplateMiss) {
		t.Errorf("expected ErrTemplateMiss, got: %v", err)
	}
}

// -------- Delete --------

func TestProvider_Delete_HappyPath(t *testing.T) {
	const (
		spaceID = "sp-4"
		dbID    = "db-to-delete"
	)
	srv := stubServer(t, map[string]http.HandlerFunc{
		"/spaces": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		},
		"/spaces/" + spaceID + "/databases/" + dbID: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				w.WriteHeader(405)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		},
	})
	defer srv.Close()

	p := newProvider(t, srv.URL)
	if err := p.Delete(context.Background(), dbID); err != nil {
		t.Errorf("Delete: %v", err)
	}
}

// -------- List --------

func TestProvider_List_HappyPath(t *testing.T) {
	const spaceID = "sp-5"
	srv := stubServer(t, map[string]http.HandlerFunc{
		"/spaces": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		},
		"/spaces/" + spaceID + "/databases": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write(dbsBody([]map[string]string{
				{"id": "d1", "name": "vxd-a", "status": "running", "dsn": "postgres://a"},
				{"id": "d2", "name": "vxd-b", "status": "running", "dsn": "postgres://b"},
			}))
		},
	})
	defer srv.Close()

	p := newProvider(t, srv.URL)
	list, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 dbs, got %d", len(list))
	}
	for _, db := range list {
		if db.Provider != "ghost" {
			t.Errorf("Provider = %q, want ghost", db.Provider)
		}
	}
}

// -------- WaitReady --------

func TestProvider_Create_WaitReady_Polls(t *testing.T) {
	const spaceID = "sp-wr"
	const dbID = "db-pending"
	var pollCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spaces":
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		case "/spaces/" + spaceID + "/databases":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": dbID, "name": "vxd-proj-a1b2c3d4-z1", "status": "provisioning",
				"dsn": "",
			})
		case "/spaces/" + spaceID + "/databases/" + dbID:
			pollCount++
			status := "provisioning"
			if pollCount >= 3 {
				status = "running"
			}
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": dbID, "name": "vxd-proj-a1b2c3d4-z1", "status": status,
				"dsn": "postgres://ready",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv.URL)
	db, err := p.Create(context.Background(), devdb.CreateOpts{
		Name:        "vxd-proj-a1b2c3d4-z1",
		WaitReady:   true,
		WaitTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Create with WaitReady: %v", err)
	}
	if db.ConnectionString != "postgres://ready" {
		t.Errorf("expected ready DSN, got %q", db.ConnectionString)
	}
	if pollCount < 3 {
		t.Errorf("expected at least 3 polls, got %d", pollCount)
	}
}

func TestProvider_Create_WaitReady_Timeout(t *testing.T) {
	const spaceID = "sp-to"
	const dbID = "db-slow"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spaces":
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		case "/spaces/" + spaceID + "/databases":
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": dbID, "name": "vxd-proj-a1b2c3d4-z1", "status": "provisioning",
			})
		case "/spaces/" + spaceID + "/databases/" + dbID:
			// Never becomes running.
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": dbID, "status": "provisioning",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv.URL)
	_, err := p.Create(context.Background(), devdb.CreateOpts{
		Name:        "vxd-proj-a1b2c3d4-z1",
		WaitReady:   true,
		WaitTimeout: 1 * time.Second, // short timeout to keep test fast
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, devdb.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown on timeout, got: %v", err)
	}
}

// -------- Schema --------

func TestProvider_Schema_NotImplemented(t *testing.T) {
	p, _ := ghost.New(ghost.Config{APIKey: "test"})
	_, err := p.Schema(context.Background(), "any-id")
	if !errors.Is(err, devdb.ErrUnsupported) {
		t.Errorf("expected ErrUnsupported from Schema, got: %v", err)
	}
}

// -------- Space resolution caching --------

func TestProvider_SpaceID_CachedAfterFirstCall(t *testing.T) {
	var spaceResolves int
	const spaceID = "sp-cached"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spaces":
			spaceResolves++
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		case "/spaces/" + spaceID + "/databases":
			w.WriteHeader(200)
			_, _ = w.Write(dbsBody(nil))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newProvider(t, srv.URL)
	_, _ = p.List(context.Background())
	_, _ = p.List(context.Background())
	_, _ = p.List(context.Background())

	if spaceResolves != 1 {
		t.Errorf("expected /spaces to be called exactly once, called %d times", spaceResolves)
	}
}

func TestProvider_SpaceID_PreConfigured_SkipsResolve(t *testing.T) {
	const spaceID = "pre-configured-space"
	var spacesCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spaces":
			spacesCalled = true
			w.WriteHeader(200)
			_, _ = w.Write(spacesBody(spaceID))
		case "/spaces/" + spaceID + "/databases":
			w.WriteHeader(200)
			_, _ = w.Write(dbsBody(nil))
		default:
			fmt.Fprintf(w, "unexpected: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p, err := ghost.New(ghost.Config{
		APIKey:  "test",
		SpaceID: spaceID,
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.List(context.Background())

	if spacesCalled {
		t.Error("expected /spaces NOT to be called when SpaceID is pre-configured")
	}
}
