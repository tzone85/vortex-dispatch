package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// SSHRunner executes agent sessions on remote machines via SSH.
type SSHRunner struct {
	host       string   // user@host
	keyFile    string   // path to SSH key (optional)
	remoteDir  string   // remote working directory base — validated absolute POSIX
	extraFlags []string // additional SSH flags
}

// SSHConfig holds configuration for the SSH runner.
type SSHConfig struct {
	Host       string   `yaml:"host"`       // user@host
	KeyFile    string   `yaml:"key_file"`   // path to private key
	RemoteDir  string   `yaml:"remote_dir"` // remote base directory (absolute POSIX path)
	ExtraFlags []string `yaml:"extra_flags"`
}

// defaultSSHRemoteDir is the fallback when SSHConfig.RemoteDir is empty.
const defaultSSHRemoteDir = "/tmp/vxd-agent"

// ValidateRemoteDir rejects remote_dir values that could traverse out of
// the intended base. Remote hosts are POSIX, so cleaning is done with
// path.Clean (forward-slash semantics) rather than filepath.Clean which
// switches separator by host OS.
//
// Rejection is done against the ORIGINAL string: `path.Clean` collapses
// `/var/lib/../../etc/cron.d` to `/etc/cron.d`, which is absolute and
// looks safe — but the operator's intent is clearly traversal. Any
// literal `..` segment in the raw input is rejected.
//
// Accepted: absolute POSIX path with no `..` segments. Empty is also
// accepted — the caller substitutes the default.
//
// Rejected: relative paths and any input containing a `..` path segment.
func ValidateRemoteDir(dir string) error {
	if dir == "" {
		return nil
	}
	if !strings.HasPrefix(dir, "/") {
		return fmt.Errorf("ssh remote_dir must be an absolute POSIX path, got %q", dir)
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == ".." {
			return fmt.Errorf("ssh remote_dir must not contain `..` traversal segments, got %q", dir)
		}
	}
	return nil
}

// NewSSHRunner creates an SSHRunner with the given config. Returns an
// error if cfg.RemoteDir fails ValidateRemoteDir — without this check a
// YAML-supplied `../../etc/cron.d` would traverse on the remote host.
func NewSSHRunner(cfg SSHConfig) (*SSHRunner, error) {
	if err := ValidateRemoteDir(cfg.RemoteDir); err != nil {
		return nil, err
	}
	remoteDir := cfg.RemoteDir
	if remoteDir == "" {
		remoteDir = defaultSSHRemoteDir
	} else {
		remoteDir = path.Clean(remoteDir)
	}
	return &SSHRunner{
		host:       cfg.Host,
		keyFile:    cfg.KeyFile,
		remoteDir:  remoteDir,
		extraFlags: cfg.ExtraFlags,
	}, nil
}

// Run uploads setup files and starts the execution on the remote machine.
func (r *SSHRunner) Run(pe PreparedExecution) error {
	// Use path.Join (POSIX-only) rather than filepath.Join — the remote is
	// always POSIX. filepath.Join would use the LOCAL OS separator, which
	// is wrong on Windows hosts dispatching to Linux remotes.
	remoteWorkDir := path.Join(r.remoteDir, pe.SessionName)

	// Create remote directory.
	if err := r.sshExec("mkdir", "-p", remoteWorkDir); err != nil {
		return fmt.Errorf("create remote dir: %w", err)
	}

	// Upload setup files via scp.
	for localPath, content := range pe.SetupFiles {
		tmpFile := filepath.Join(os.TempDir(), filepath.Base(localPath))
		if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}
		defer os.Remove(tmpFile)

		remotePath := path.Join(remoteWorkDir, filepath.Base(localPath))
		if err := r.scpTo(tmpFile, remotePath); err != nil {
			return fmt.Errorf("scp setup file %s: %w", localPath, err)
		}
	}

	// Build env exports — values are POSIX single-quoted so attacker-
	// controlled DSNs or YAML config cannot inject shell commands.
	envExports := BuildEnvExports(pe.Env)

	// Execute command remotely in background (nohup + disown).
	remoteCmd := fmt.Sprintf("cd %s && %s nohup sh -c %q > /dev/null 2>&1 &",
		remoteWorkDir, envExports, pe.Command)

	if err := r.sshExec("sh", "-c", remoteCmd); err != nil {
		return fmt.Errorf("ssh exec: %w", err)
	}

	return nil
}

// Terminate kills the remote process by session ID pattern.
// Validates sessionID before composing the pkill command — sessionIDs
// are internally generated today (ULID + role + counter), but the
// defence-in-depth check stops a regression in the ID-generation path
// from becoming a remote command injection.
func (r *SSHRunner) Terminate(sessionID string) error {
	if err := ValidateSessionName(sessionID); err != nil {
		return fmt.Errorf("ssh terminate: %w", err)
	}
	cmd := fmt.Sprintf("pkill -f %q 2>/dev/null || true", sessionID)
	return r.sshExec("sh", "-c", cmd)
}

// SendInput is not supported for SSH runner.
func (r *SSHRunner) SendInput(sessionID string, input string) error {
	return fmt.Errorf("SendInput not supported for SSH runner")
}

// ReadOutput reads the last N lines from the remote log file. Validates
// sessionID to keep `path.Join` from emitting a remote path that
// includes shell metacharacters or traversal segments.
func (r *SSHRunner) ReadOutput(sessionID string, lines int) (string, error) {
	if err := ValidateSessionName(sessionID); err != nil {
		return "", fmt.Errorf("ssh read output: %w", err)
	}
	logPath := path.Join(r.remoteDir, sessionID, "agent.log")
	cmd := r.buildSSHCmd("tail", fmt.Sprintf("-%d", lines), logPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ssh tail: %w", err)
	}
	return string(out), nil
}

// IsAlive checks if the remote process is still running.
// Returns false (not "alive") on an invalid sessionID so a future bug
// in the ID generator surfaces as "dead session" rather than reaching
// `pgrep -f` with attacker-influenced content.
func (r *SSHRunner) IsAlive(sessionID string) bool {
	if ValidateSessionName(sessionID) != nil {
		return false
	}
	cmd := r.buildSSHCmd("pgrep", "-f", sessionID)
	return cmd.Run() == nil
}

// sshExec runs a command on the remote host and returns any error.
func (r *SSHRunner) sshExec(args ...string) error {
	cmd := r.buildSSHCmd(args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// buildSSHCmd constructs an SSH command with the runner's config.
func (r *SSHRunner) buildSSHCmd(remoteArgs ...string) *exec.Cmd {
	sshArgs := []string{}
	if r.keyFile != "" {
		sshArgs = append(sshArgs, "-i", r.keyFile)
	}
	sshArgs = append(sshArgs, r.extraFlags...)
	sshArgs = append(sshArgs, r.host)
	sshArgs = append(sshArgs, remoteArgs...)
	return sshExecCommand("ssh", sshArgs...)
}

// scpTo uploads a local file to the remote host.
func (r *SSHRunner) scpTo(localPath, remotePath string) error {
	scpArgs := []string{}
	if r.keyFile != "" {
		scpArgs = append(scpArgs, "-i", r.keyFile)
	}
	scpArgs = append(scpArgs, localPath, r.host+":"+remotePath)
	cmd := sshExecCommand("scp", scpArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("scp: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sshExecCommand wraps exec.Command for testability (allows mocking in tests).
var sshExecCommand = exec.Command
