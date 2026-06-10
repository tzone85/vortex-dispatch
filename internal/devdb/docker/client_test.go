package docker_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/docker"
)

func TestClient_Ping_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("OK"))
	}))
	defer srv.Close()

	c := docker.NewClient(docker.ClientConfig{BaseURL: srv.URL})
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestClient_Ping_Unreachable(t *testing.T) {
	c := docker.NewClient(docker.ClientConfig{BaseURL: "http://127.0.0.1:1"})
	err := c.Ping(context.Background())
	if !errors.Is(err, devdb.ErrProviderDown) {
		t.Errorf("Ping(unreachable) = %v, want ErrProviderDown", err)
	}
}

func TestClient_InspectContainer_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/vxd-devdb-pg16/json" {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"no such container"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := docker.NewClient(docker.ClientConfig{BaseURL: srv.URL})
	state, err := c.InspectContainer(context.Background(), "vxd-devdb-pg16")
	if err != nil {
		t.Errorf("InspectContainer NotFound should not error, got %v", err)
	}
	if state.Exists {
		t.Errorf("expected Exists=false")
	}
}

func TestClient_InspectContainer_Running(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/vxd-devdb-pg16/json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"State":{"Running":true}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := docker.NewClient(docker.ClientConfig{BaseURL: srv.URL})
	state, err := c.InspectContainer(context.Background(), "vxd-devdb-pg16")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists || !state.Running {
		t.Errorf("state = %+v, want Exists=true Running=true", state)
	}
}

func TestClient_CreateContainer_BindsLoopbackByDefault(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/create" {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"abc123"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := docker.NewClient(docker.ClientConfig{BaseURL: srv.URL})
	_, err := c.CreateContainer(context.Background(), docker.CreateContainerSpec{
		Name:          "vxd-devdb-pg16",
		Image:         "postgres:16",
		HostPort:      5500,
		AdminPassword: "pw",
		// HostBindIP left empty → must default to loopback.
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotBody), `"HostIp":"127.0.0.1"`) {
		t.Errorf("CreateContainer body must bind to loopback by default; got: %s", gotBody)
	}
}

func TestClient_CreateContainer_RespectsExplicitBindIP(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/create" {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"abc123"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := docker.NewClient(docker.ClientConfig{BaseURL: srv.URL})
	_, err := c.CreateContainer(context.Background(), docker.CreateContainerSpec{
		Name: "vxd-devdb-pg16", Image: "postgres:16", HostPort: 5500,
		AdminPassword: "pw", HostBindIP: "0.0.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotBody), `"HostIp":"0.0.0.0"`) {
		t.Errorf("CreateContainer body should honor explicit HostBindIP; got: %s", gotBody)
	}
}
