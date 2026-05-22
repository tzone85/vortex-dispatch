//go:build !integration

package docker_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/docker"
)

func TestProvider_Name(t *testing.T) {
	p := docker.NewProvider(docker.Config{Image: "postgres:16", HostPortRange: "5500-5500"})
	if p.Name() != "docker" {
		t.Errorf("Name = %q, want docker", p.Name())
	}
}

func TestProvider_SatisfiesInterface(t *testing.T) {
	var _ devdb.Provider = docker.NewProvider(docker.Config{HostPortRange: "5500-5599"})
}
