package dashstart

import (
	"fmt"
	"os/exec"
)

// SpawnArgs captures the inputs needed to construct an exec.Cmd that, when
// started, launches `vxd dashboard --web` as a detached background daemon.
//
// The struct is exported so test code can build a spec, call BuildCmd, and
// inspect the Cmd without ever calling Start().
type SpawnArgs struct {
	Self          string // absolute path to the vxd binary
	Port          int    // web port; passed via --port
	Pidfile       string // path the daemon writes its PID to
	BootstrapFile string // path the daemon writes its initial bootstrap nonce to
	NoOpen        bool   // tell the daemon not to open a browser itself; the caller does that
}

// BuildCmd returns the exec.Cmd that, when started, runs:
//
//	vxd dashboard --web --port=<port> --pidfile=<...> --bootstrap-file=<...> [--no-open]
//
// The function does NOT call cmd.Start. The caller is responsible for
// attaching stdio, applying platform-specific detach flags, and starting
// the process. This split keeps BuildCmd a pure construction function.
func BuildCmd(args SpawnArgs) (*exec.Cmd, error) {
	if args.Self == "" {
		return nil, fmt.Errorf("dashstart: Self path is required")
	}
	if args.Port <= 0 {
		return nil, fmt.Errorf("dashstart: Port must be > 0")
	}
	if args.Pidfile == "" {
		return nil, fmt.Errorf("dashstart: Pidfile is required")
	}
	if args.BootstrapFile == "" {
		return nil, fmt.Errorf("dashstart: BootstrapFile is required")
	}

	cmdArgs := []string{
		"dashboard",
		"--web",
		fmt.Sprintf("--port=%d", args.Port),
		fmt.Sprintf("--pidfile=%s", args.Pidfile),
		fmt.Sprintf("--bootstrap-file=%s", args.BootstrapFile),
	}
	if args.NoOpen {
		cmdArgs = append(cmdArgs, "--no-open")
	}

	cmd := exec.Command(args.Self, cmdArgs...)
	cmd.Env = FilteredEnv()
	return cmd, nil
}
