package runtime

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestNewRunnerFromConfig_DefaultIsTmux(t *testing.T) {
	r, err := NewRunnerFromConfig(config.RuntimeConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*TmuxRunner); !ok {
		t.Errorf("expected *TmuxRunner, got %T", r)
	}
}

func TestNewRunnerFromConfig_ExplicitTmux(t *testing.T) {
	r, err := NewRunnerFromConfig(config.RuntimeConfig{Runner: "tmux"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*TmuxRunner); !ok {
		t.Errorf("expected *TmuxRunner, got %T", r)
	}
}

func TestNewRunnerFromConfig_Docker(t *testing.T) {
	r, err := NewRunnerFromConfig(config.RuntimeConfig{
		Runner: "docker",
		Docker: config.DockerRunnerConfig{Image: "vxd-agent:latest"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*DockerRunner); !ok {
		t.Errorf("expected *DockerRunner, got %T", r)
	}
}

func TestNewRunnerFromConfig_DockerMissingImage(t *testing.T) {
	_, err := NewRunnerFromConfig(config.RuntimeConfig{Runner: "docker"})
	if err == nil {
		t.Error("expected error for missing docker.image")
	}
}

func TestNewRunnerFromConfig_SSH(t *testing.T) {
	r, err := NewRunnerFromConfig(config.RuntimeConfig{
		Runner: "ssh",
		SSH:    config.SSHRunnerConfig{Host: "user@example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*SSHRunner); !ok {
		t.Errorf("expected *SSHRunner, got %T", r)
	}
}

func TestNewRunnerFromConfig_SSHMissingHost(t *testing.T) {
	_, err := NewRunnerFromConfig(config.RuntimeConfig{Runner: "ssh"})
	if err == nil {
		t.Error("expected error for missing ssh.host")
	}
}

func TestNewRunnerFromConfig_UnknownRunner(t *testing.T) {
	_, err := NewRunnerFromConfig(config.RuntimeConfig{Runner: "kubernetes"})
	if err == nil {
		t.Error("expected error for unknown runner type")
	}
}
