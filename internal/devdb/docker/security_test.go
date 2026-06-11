package docker

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLoadOrCreateAdminPassword_TightensExistingPerms pins the
// defence-in-depth behaviour added by the 2026-06-11 audit follow-up:
// an existing password file with lax perms (e.g. world-readable) is
// re-chmodded to 0o600 on read.
func TestLoadOrCreateAdminPassword_TightensExistingPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics not available on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "devdb-admin.pw")
	if err := os.WriteFile(path, []byte("seeded-password\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}

	p := &Provider{}
	got, err := p.loadOrCreateAdminPassword(dir)
	if err != nil {
		t.Fatalf("loadOrCreateAdminPassword: %v", err)
	}
	if got != "seeded-password" {
		t.Errorf("password = %q, want seeded-password", got)
	}

	if info, err := os.Stat(path); err == nil {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("file perm = %o after load, want 600", perm)
		}
	} else {
		t.Fatalf("stat file: %v", err)
	}
	if info, err := os.Stat(dir); err == nil {
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("dir perm = %o after load, want 700", perm)
		}
	} else {
		t.Fatalf("stat dir: %v", err)
	}
}

// TestLoadOrCreateAdminPassword_GeneratesFreshWithSafePerms covers the
// happy path: no existing file, fresh password written 0o600.
func TestLoadOrCreateAdminPassword_GeneratesFreshWithSafePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics not available on Windows")
	}

	dir := filepath.Join(t.TempDir(), "fresh")
	p := &Provider{}

	pw, err := p.loadOrCreateAdminPassword(dir)
	if err != nil {
		t.Fatalf("loadOrCreateAdminPassword: %v", err)
	}
	if len(pw) != 32 {
		t.Errorf("password length = %d, want 32 (hex of 16 bytes)", len(pw))
	}

	info, err := os.Stat(filepath.Join(dir, "devdb-admin.pw"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600", perm)
	}
}

// TestPGConn_CreateDB_RejectsInvalidName pins the SQL-identifier
// defence: db names that don't pass devdb.IsValid are rejected before
// they reach Postgres. The validator lives in internal/devdb/naming.go
// and constrains identifiers to [a-z][a-z0-9-]{0,62}.
func TestPGConn_CreateDB_RejectsInvalidName(t *testing.T) {
	bad := []string{
		``,                                        // empty
		`Foo`,                                     // uppercase
		`1bad-start`,                              // leading digit
		`with space`,                              // space
		`with"quote`,                              // embedded quote
		`with;semicolon`,                          // semicolon
		`-leading-hyphen`,                         // hyphen start
		"verylonglonglonglonglonglonglonglonglonglonglonglonglonglonglongname", // > 63 chars (no hyphens)
	}
	p := &PGConn{} // nil pgx.Conn — Exec never called because validation fails first.
	for _, name := range bad {
		t.Run(name, func(t *testing.T) {
			if err := p.CreateDB(context.Background(), name); err == nil {
				t.Errorf("expected error for invalid name %q, got nil", name)
			}
		})
	}
}

func TestPGConn_DropDB_RejectsInvalidName(t *testing.T) {
	p := &PGConn{}
	if err := p.DropDB(context.Background(), `bad;name`); err == nil {
		t.Error("expected error for invalid name in DropDB")
	}
}

func TestPGConn_CreateDBFromTemplate_RejectsInvalidNames(t *testing.T) {
	p := &PGConn{}
	if err := p.CreateDBFromTemplate(context.Background(), `bad name`, `vxd-base`); err == nil {
		t.Error("expected error for invalid db name")
	}
	if err := p.CreateDBFromTemplate(context.Background(), `vxd-a`, `BadTemplate`); err == nil {
		t.Error("expected error for invalid template name")
	}
}
