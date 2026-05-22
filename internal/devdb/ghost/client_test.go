package ghost_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/ghost"
)

// healthHandler returns 200 for GET /health, 404 otherwise.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/health" {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func TestClient_Ping_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(healthHandler))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test-key"})
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping on healthy server: %v", err)
	}
}

func TestClient_Ping_SetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "my-secret-key"})
	_ = c.Ping(context.Background())
	if gotAuth != "Bearer my-secret-key" {
		t.Errorf("expected 'Bearer my-secret-key', got %q", gotAuth)
	}
}

func TestClient_Ping_AuthFails_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "bad"})
	err := c.Ping(context.Background())
	if !errors.Is(err, devdb.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown on 401, got: %v", err)
	}
}

func TestClient_Ping_AuthFails_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "bad"})
	err := c.Ping(context.Background())
	if !errors.Is(err, devdb.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown on 403, got: %v", err)
	}
}

func TestClient_Ping_RetriesOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test", Timeout: 5 * time.Second})
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("expected success after retry, got: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected 2 calls (initial + 1 retry), got %d", calls)
	}
}

func TestClient_Ping_NoRetryOn4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	_ = c.Ping(context.Background())
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected exactly 1 call for 4xx (no retry), got %d", calls)
	}
}

func TestClient_Ping_RateLimited_429_ReturnsProviderDown(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test", Timeout: 5 * time.Second})
	err := c.Ping(context.Background())
	if !errors.Is(err, devdb.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown on 429, got: %v", err)
	}
}

func TestClient_CreateDB_HappyPath(t *testing.T) {
	const spaceID = "sp-test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		expected := "/spaces/" + spaceID + "/databases"
		if r.URL.Path != expected {
			t.Errorf("expected path %s, got %s", expected, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": "db-abc123", "name": "vxd-myproj-a1b2c3d4-z1", "status": "running",
			"dsn": "postgres://user:pass@host/db",
		})
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	d, err := c.CreateDB(context.Background(), spaceID, "vxd-myproj-a1b2c3d4-z1")
	if err != nil {
		t.Fatalf("CreateDB: %v", err)
	}
	if d.ID != "db-abc123" {
		t.Errorf("expected id db-abc123, got %q", d.ID)
	}
	if d.DSN != "postgres://user:pass@host/db" {
		t.Errorf("expected DSN, got %q", d.DSN)
	}
}

func TestClient_CreateDB_Conflict_409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"already exists"}`))
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	_, err := c.CreateDB(context.Background(), "sp1", "vxd-myproj-a1b2c3d4-z1")
	if !errors.Is(err, devdb.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists on 409, got: %v", err)
	}
}

func TestClient_ForkDB_HappyPath(t *testing.T) {
	const (
		spaceID  = "sp-fork"
		tplRef   = "tpl-ref-123"
		forkName = "vxd-fork-a1b2c3d4-z1"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "/spaces/" + spaceID + "/databases/" + tplRef + "/fork"
		if r.URL.Path != expected {
			t.Errorf("expected path %s, got %s", expected, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": "db-fork1", "name": forkName, "status": "running",
			"dsn": "postgres://u:p@h/db-fork1",
		})
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	d, err := c.ForkDB(context.Background(), spaceID, tplRef, forkName)
	if err != nil {
		t.Fatalf("ForkDB: %v", err)
	}
	if d.ID != "db-fork1" {
		t.Errorf("expected id db-fork1, got %q", d.ID)
	}
}

func TestClient_DeleteDB_HappyPath(t *testing.T) {
	const (
		spaceID = "sp-del"
		dbRef   = "db-del-ref"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	if err := c.DeleteDB(context.Background(), spaceID, dbRef); err != nil {
		t.Errorf("DeleteDB: %v", err)
	}
}

func TestClient_DeleteDB_NotFound_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	err := c.DeleteDB(context.Background(), "sp1", "missing-db")
	if !errors.Is(err, devdb.ErrNotFound) {
		t.Errorf("expected ErrNotFound on 404 delete, got: %v", err)
	}
}

func TestClient_ListDBs_HappyPath(t *testing.T) {
	const spaceID = "sp-list"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"databases": []map[string]string{
				{"id": "d1", "name": "vxd-p1-abc-z1", "status": "running", "dsn": "postgres://a"},
				{"id": "d2", "name": "vxd-p2-def-z2", "status": "running", "dsn": "postgres://b"},
			},
		})
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	list, err := c.ListDBs(context.Background(), spaceID)
	if err != nil {
		t.Fatalf("ListDBs: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 dbs, got %d", len(list))
	}
	if list[0].ID != "d1" {
		t.Errorf("expected first ID d1, got %q", list[0].ID)
	}
}

func TestClient_ResolveSpaceID_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spaces" {
			t.Errorf("expected /spaces, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"spaces": []map[string]string{
				{"id": "sp-first"},
				{"id": "sp-second"},
			},
		})
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	id, err := c.ResolveSpaceID(context.Background())
	if err != nil {
		t.Fatalf("ResolveSpaceID: %v", err)
	}
	if id != "sp-first" {
		t.Errorf("expected first space id sp-first, got %q", id)
	}
}

func TestClient_ResolveSpaceID_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"spaces": []interface{}{}})
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test"})
	_, err := c.ResolveSpaceID(context.Background())
	if err == nil {
		t.Error("expected error when no spaces returned")
	}
}

func TestClient_ServerError_500_MapsToProviderDown(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 500 so retry exhausts.
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	c := ghost.NewClient(ghost.ClientConfig{BaseURL: srv.URL, APIKey: "test", Timeout: 5 * time.Second})
	err := c.Ping(context.Background())
	if !errors.Is(err, devdb.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown on persistent 500, got: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected 2 calls for 500 (1 retry), got %d", calls)
	}
}
