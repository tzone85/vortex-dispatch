package docker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
