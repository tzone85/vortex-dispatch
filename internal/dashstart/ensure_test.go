package dashstart

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// recordingSpawner captures Spawn() arguments without forking.
type recordingSpawner struct {
	called  bool
	args    SpawnArgs
	logPath string
	err     error
}

func (r *recordingSpawner) Spawn(args SpawnArgs, logPath string) (int, error) {
	r.called = true
	r.args = args
	r.logPath = logPath
	if r.err != nil {
		return -1, r.err
	}
	return 9999, nil
}

func TestEnsure_ReusesAliveDaemon(t *testing.T) {
	dir := t.TempDir()
	// pretend a daemon is already running — drop a pidfile + bootstrap file
	if err := os.WriteFile(filepath.Join(dir, "dashboard.pid"), []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dashboard.bootstrap"), []byte("nonce-abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	doer := &stubDoer{status: 200}
	sp := &recordingSpawner{}

	h, err := Ensure(context.Background(), Config{
		Self:     "/vxd",
		StateDir: dir,
		Port:     8787,
		Doer:     doer,
		Spawner:  sp,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sp.called {
		t.Error("Spawn should NOT have been called when daemon is alive")
	}
	if !h.Reused {
		t.Error("Handle.Reused should be true")
	}
	if h.PID != 4242 {
		t.Errorf("PID = %d, want 4242", h.PID)
	}
	if h.BootstrapNonce != "nonce-abc" {
		t.Errorf("BootstrapNonce = %q, want nonce-abc", h.BootstrapNonce)
	}
}

func TestEnsure_SpawnsWhenDead(t *testing.T) {
	dir := t.TempDir()
	// healthz first returns connection-refused, then 200 after Spawn is called.
	doer := &programmedDoer{statuses: []int{0, 200}, errs: []error{errors.New("refused"), nil}}
	sp := &recordingSpawner{}

	// Pre-write a bootstrap file so Ensure can read it after WaitHealthy
	// without us having to wire up a real server.
	if err := os.WriteFile(filepath.Join(dir, "dashboard.bootstrap"), []byte("post-spawn-nonce\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h, err := Ensure(context.Background(), Config{
		Self:     "/vxd",
		StateDir: dir,
		Port:     8787,
		Doer:     doer,
		Spawner:  sp,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !sp.called {
		t.Fatal("Spawn should have been called")
	}
	if h.Reused {
		t.Error("Handle.Reused should be false on fresh spawn")
	}
	if h.PID != 9999 {
		t.Errorf("PID = %d, want 9999", h.PID)
	}
	if h.BootstrapNonce != "post-spawn-nonce" {
		t.Errorf("BootstrapNonce = %q, want post-spawn-nonce", h.BootstrapNonce)
	}
	if sp.args.Pidfile == "" || sp.args.BootstrapFile == "" {
		t.Errorf("Spawn args missing pidfile/bootstrap-file: %+v", sp.args)
	}
}

func TestEnsure_AbortsOnSpawnError(t *testing.T) {
	dir := t.TempDir()
	doer := &stubDoer{err: errors.New("refused")}
	sp := &recordingSpawner{err: errors.New("fork: too many open files")}

	_, err := Ensure(context.Background(), Config{
		Self:     "/vxd",
		StateDir: dir,
		Port:     8787,
		Doer:     doer,
		Spawner:  sp,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// programmedDoer cycles through a list of canned responses, advancing one
// slot per call. Used to model "first probe fails, second succeeds".
type programmedDoer struct {
	statuses []int
	errs     []error
	idx      int
}

func (p *programmedDoer) Do(req *http.Request) (*http.Response, error) {
	defer func() {
		if p.idx < len(p.statuses)-1 {
			p.idx++
		}
	}()
	if err := p.errs[p.idx]; err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: p.statuses[p.idx], Body: http.NoBody}, nil
}
