package cli

import (
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestNewDevDBProvider_NullDefault(t *testing.T) {
	prov, err := newDevDBProvider(config.Config{})
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if prov == nil {
		t.Error("expected null provider for empty config")
	}
}

func TestNewDevDBProvider_NullExplicit(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "null"
	prov, err := newDevDBProvider(cfg)
	if err != nil {
		t.Fatalf("null: %v", err)
	}
	if prov == nil {
		t.Error("expected null provider")
	}
}

func TestNewDevDBProvider_Docker(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "docker"
	cfg.DevDB.Docker.Image = "postgres:16"
	cfg.DevDB.Docker.HostPortRange = "5500-5599"
	prov, err := newDevDBProvider(cfg)
	if err != nil {
		t.Fatalf("docker: %v", err)
	}
	if prov == nil {
		t.Error("expected docker provider")
	}
}

func TestNewDevDBProvider_GhostNoAPIKey(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "ghost"
	cfg.DevDB.Ghost.APIKeyEnv = "VXD_NO_SUCH_KEY_FOR_TEST"
	_, err := newDevDBProvider(cfg)
	if err == nil {
		t.Error("expected error for missing ghost API key")
	}
}

func TestNewDevDBProvider_UnknownProvider(t *testing.T) {
	cfg := config.Config{}
	cfg.DevDB.Provider = "definitely-not-a-real-provider"
	_, err := newDevDBProvider(cfg)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "not recognised") {
		t.Errorf("expected 'not recognised' in error, got: %v", err)
	}
}
