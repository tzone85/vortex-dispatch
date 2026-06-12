package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/devdb"
)

func TestFindDBByNameOrID_ByName(t *testing.T) {
	dbs := []devdb.DB{
		{ID: "id-1", Name: "alpha"},
		{ID: "id-2", Name: "beta"},
	}
	got, err := findDBByNameOrID(dbs, "beta")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ID != "id-2" {
		t.Errorf("got ID %q, want id-2", got.ID)
	}
}

func TestFindDBByNameOrID_ByID(t *testing.T) {
	dbs := []devdb.DB{{ID: "id-x", Name: "foo"}}
	got, err := findDBByNameOrID(dbs, "id-x")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Name != "foo" {
		t.Errorf("got name %q, want foo", got.Name)
	}
}

func TestFindDBByNameOrID_NotFound(t *testing.T) {
	if _, err := findDBByNameOrID([]devdb.DB{{ID: "x", Name: "y"}}, "missing"); err == nil {
		t.Error("expected error for unknown name/id")
	}
}

func TestFindDBByNameOrID_EmptySlice(t *testing.T) {
	if _, err := findDBByNameOrID(nil, "anything"); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestIsTerminal_BufferIsNotTerminal(t *testing.T) {
	// bytes.Buffer is io.Writer but not *os.File — should report false.
	var buf bytes.Buffer
	if isTerminal(&buf) {
		t.Error("bytes.Buffer should not be detected as a terminal")
	}
}

func TestIsTerminal_RegularFileIsNotTerminal(t *testing.T) {
	// A regular file on disk has mode bits but no os.ModeCharDevice.
	f, err := os.CreateTemp("", "vxd-tty-test-*")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if isTerminal(f) {
		t.Error("regular file should not be detected as a terminal")
	}
}

// dbProviderFor + dockerProviderFor go through loadStores; the failure
// path (devdb disabled / wrong provider) is what we can hit without
// docker. Both share the projectRuntimeConfig fallback.
func TestDBProviderFor_DevDBDisabled(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := seedVxdYaml(t, stateDir)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "default", "")
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config: %v", err)
	}

	_, err := dbProviderFor(cmd)
	if err == nil {
		t.Fatal("expected error when devdb not configured")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' in error, got: %v", err)
	}
}

func TestDockerProviderFor_NonDockerProvider(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := seedVxdYaml(t, stateDir)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("project", "default", "")
	if err := cmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("set config: %v", err)
	}

	_, err := dockerProviderFor(cmd)
	if err == nil {
		t.Fatal("expected error when provider is not docker")
	}
	if !strings.Contains(err.Error(), "docker") {
		t.Errorf("expected 'docker' in error, got: %v", err)
	}
}
