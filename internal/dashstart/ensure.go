package dashstart

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config carries the inputs the orchestrator needs to reuse-or-launch the
// dashboard daemon. The caller (cli.runReq) is expected to fill Self,
// StateDir, Port; the rest have safe defaults.
type Config struct {
	Self           string        // path to vxd binary (defaults to os.Args[0])
	StateDir       string        // ~/.vxd (or override) — pidfile + bootstrap-file land under here
	Port           int           // dashboard port; default 8787
	HealthzTimeout time.Duration // total time to wait for a fresh spawn (default 3s)
	HealthzPoll    time.Duration // poll interval (default 100ms)
	Doer           HTTPDoer      // override for tests; default http.DefaultClient
	Spawner        Spawner       // override for tests; default RealSpawner
	NoOpen         bool          // tell the daemon not to also try opening a browser
}

// Handle describes a running (or freshly launched) dashboard daemon.
type Handle struct {
	PID            int
	Port           int
	BootstrapNonce string
	URL            string // base URL of the dashboard, e.g. http://localhost:8787
	Reused         bool   // true iff the daemon was already running and we joined it
}

// Spawner abstracts the actual process launch. Tests pass a stub that records
// args and returns a fake PID; production uses RealSpawner which Starts an
// exec.Cmd with ApplyDetach and stdio redirected to a log file.
type Spawner interface {
	Spawn(args SpawnArgs, logPath string) (pid int, err error)
}

// RealSpawner is the production Spawner. It opens logPath for append, applies
// platform-specific detach flags, and Starts the command. It does NOT Wait
// on the process.
type RealSpawner struct{}

// Spawn implements Spawner.
func (RealSpawner) Spawn(args SpawnArgs, logPath string) (int, error) {
	cmd, err := BuildCmd(args)
	if err != nil {
		return -1, err
	}
	ApplyDetach(cmd)

	var logFile *os.File
	if logPath != "" {
		if mkErr := os.MkdirAll(filepath.Dir(logPath), 0o755); mkErr != nil {
			return -1, fmt.Errorf("mkdir log dir: %w", mkErr)
		}
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return -1, fmt.Errorf("open log file: %w", err)
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return -1, fmt.Errorf("open /dev/null: %w", err)
	}
	cmd.Stdin = devNull

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		_ = devNull.Close()
		return -1, fmt.Errorf("start daemon: %w", err)
	}

	// We can close our copies; the child has its own fds via Start.
	if logFile != nil {
		_ = logFile.Close()
	}
	_ = devNull.Close()

	return cmd.Process.Pid, nil
}

// PidfilePath returns the canonical pidfile path under StateDir.
func PidfilePath(stateDir string) string {
	return filepath.Join(stateDir, "dashboard.pid")
}

// BootstrapFilePath returns the canonical bootstrap-file path under StateDir.
func BootstrapFilePath(stateDir string) string {
	return filepath.Join(stateDir, "dashboard.bootstrap")
}

// LogPath returns the canonical log file used for dashboard daemon stdio.
func LogPath(stateDir string) string {
	return filepath.Join(stateDir, "logs", "dashboard.log")
}

// Ensure probes for an existing daemon and, if none answers, spawns a new
// one. Returns a Handle describing how the caller should reach it.
//
// Ensure is the only function in this package that has side effects in
// production (process spawn, file IO). All the heavy lifting is delegated
// to interfaces so tests can drive it without touching the OS.
func Ensure(ctx context.Context, cfg Config) (Handle, error) {
	cfg = withDefaults(cfg)

	pidfile := PidfilePath(cfg.StateDir)
	bootstrapFile := BootstrapFilePath(cfg.StateDir)

	if info, err := IsAlive(ctx, cfg.Doer, pidfile, cfg.Port); err == nil {
		nonce, nerr := readBootstrap(bootstrapFile)
		if nerr != nil {
			// Daemon is alive but the file is gone — happens if a previous
			// caller consumed the nonce already. We still return a handle;
			// the user can hit /?bootstrap= via cookie session.
			nonce = ""
		}
		return Handle{
			PID:            info.PID,
			Port:           info.Port,
			BootstrapNonce: nonce,
			URL:            fmt.Sprintf("http://localhost:%d", info.Port),
			Reused:         true,
		}, nil
	}

	// No daemon running — make sure the state dir exists, then spawn.
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return Handle{}, fmt.Errorf("mkdir state dir: %w", err)
	}

	pid, err := cfg.Spawner.Spawn(SpawnArgs{
		Self:          cfg.Self,
		Port:          cfg.Port,
		Pidfile:       pidfile,
		BootstrapFile: bootstrapFile,
		NoOpen:        cfg.NoOpen,
	}, LogPath(cfg.StateDir))
	if err != nil {
		return Handle{}, fmt.Errorf("spawn dashboard daemon: %w", err)
	}

	if _, err := WaitHealthy(ctx, cfg.Doer, pidfile, cfg.Port, cfg.HealthzPoll, cfg.HealthzTimeout); err != nil {
		return Handle{}, err
	}

	// At this point the daemon has written the bootstrap file before
	// answering healthz (server.go orders it that way). Read it.
	nonce, _ := readBootstrap(bootstrapFile)

	return Handle{
		PID:            pid,
		Port:           cfg.Port,
		BootstrapNonce: nonce,
		URL:            fmt.Sprintf("http://localhost:%d", cfg.Port),
		Reused:         false,
	}, nil
}

func withDefaults(cfg Config) Config {
	if cfg.Self == "" {
		cfg.Self = os.Args[0]
	}
	if cfg.Port == 0 {
		cfg.Port = 8787
	}
	if cfg.HealthzTimeout == 0 {
		cfg.HealthzTimeout = 3 * time.Second
	}
	if cfg.HealthzPoll == 0 {
		cfg.HealthzPoll = 100 * time.Millisecond
	}
	if cfg.Doer == nil {
		cfg.Doer = &http.Client{Timeout: 500 * time.Millisecond}
	}
	if cfg.Spawner == nil {
		cfg.Spawner = RealSpawner{}
	}
	return cfg
}

func readBootstrap(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
