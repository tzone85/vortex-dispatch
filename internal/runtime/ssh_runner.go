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

// dangerousSSHOptions are OpenSSH client options that allow arbitrary
// local code execution. Any `-o name=value` or `-o name` element whose
// option name appears here is rejected.
//
//   - ProxyCommand — runs the supplied binary before connecting.
//   - LocalCommand — runs after auth completes.
//   - PermitLocalCommand — enables LocalCommand.
//   - KnownHostsCommand — runs to list known_hosts entries (RCE).
//   - ProxyJump (`-J` shortcut) — chained ProxyCommand under the hood.
//   - Match / Host — block-config injection.
//
// Common legitimate options (`StrictHostKeyChecking`, `UserKnownHostsFile`,
// `ServerAliveInterval`, `ConnectTimeout`, etc.) remain allowed.
var dangerousSSHOptions = map[string]struct{}{
	"proxycommand":       {},
	"localcommand":       {},
	"permitlocalcommand": {},
	"knownhostscommand":  {},
	"proxyjump":          {},
	"match":              {},
	"host":               {},
	"include":            {},
}

// dangerousSSHFlags are bare SSH client flags that are themselves
// dangerous regardless of the option following them (`-F` substitutes
// the entire client config; `-J` is the ProxyJump shortcut).
var dangerousSSHFlags = map[string]struct{}{
	"-F": {},
	"-J": {},
}

// ValidateSSHExtraFlags rejects elements from ssh.extra_flags that
// could turn the SSH command into local code execution. Specifically:
//
//   - `-F` and `-J` are rejected outright (config file injection + ProxyJump).
//   - `-o name=value` or `-o name value` is rejected when `name` is on
//     the dangerousSSHOptions set above (case-insensitive — OpenSSH
//     matches option names case-insensitively).
//
// Common legitimate flags (`-p`, `-i`, `-4`, `-6`, `-T`, `-v`, `-q`,
// `-C`) and benign `-o` options (StrictHostKeyChecking, UserKnownHosts-
// File, ServerAliveInterval, etc.) remain allowed.
func ValidateSSHExtraFlags(flags []string) error {
	for i := 0; i < len(flags); i++ {
		f := flags[i]
		if _, bad := dangerousSSHFlags[f]; bad {
			return fmt.Errorf("ssh.extra_flags[%d] %q is not permitted (could execute arbitrary local code via config substitution / ProxyJump)", i, f)
		}
		// -o name=value (combined) or -o name (next slot is value)
		if f == "-o" && i+1 < len(flags) {
			name := optionName(flags[i+1])
			if _, bad := dangerousSSHOptions[strings.ToLower(name)]; bad {
				return fmt.Errorf("ssh.extra_flags[%d:%d] -o %s is not permitted (executes arbitrary local code)", i, i+1, name)
			}
			i++ // skip value slot
			continue
		}
		if strings.HasPrefix(f, "-o") && len(f) > 2 {
			name := optionName(strings.TrimPrefix(f, "-o"))
			if _, bad := dangerousSSHOptions[strings.ToLower(name)]; bad {
				return fmt.Errorf("ssh.extra_flags[%d] %q is not permitted (executes arbitrary local code)", i, f)
			}
		}
	}
	return nil
}

// optionName extracts the bare option name from an SSH -o argument
// (e.g. "ProxyCommand=foo" → "ProxyCommand", "StrictHostKeyChecking" →
// "StrictHostKeyChecking"). Trims leading whitespace and equals values.
func optionName(s string) string {
	s = strings.TrimSpace(s)
	if eq := strings.Index(s, "="); eq >= 0 {
		s = s[:eq]
	}
	return s
}

// NewSSHRunner creates an SSHRunner with the given config. Returns an
// error if cfg.RemoteDir fails ValidateRemoteDir, or if cfg.ExtraFlags
// contains a dangerous OpenSSH option flag (see ValidateSSHExtraFlags).
func NewSSHRunner(cfg SSHConfig) (*SSHRunner, error) {
	if err := ValidateRemoteDir(cfg.RemoteDir); err != nil {
		return nil, err
	}
	if err := ValidateSSHExtraFlags(cfg.ExtraFlags); err != nil {
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
	// Validate SessionName before composing the remote work directory.
	// Terminate/ReadOutput/IsAlive already do this; Run was the
	// remaining inconsistency the final audit caught. A regression in
	// the ID-generation path that produced a session name containing
	// `&&`, spaces, or shell metas would otherwise break out of the
	// `cd %s` argument below.
	if err := ValidateSessionName(pe.SessionName); err != nil {
		return fmt.Errorf("ssh run: %w", err)
	}
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
		// Setup files may carry prompt content (which can include the
		// project's WAVE_CONTEXT, acceptance criteria, secrets pulled
		// from the env). Write 0o600 so other users on the dispatch
		// host cannot read them during the SCP window.
		tmpFile := filepath.Join(os.TempDir(), filepath.Base(localPath))
		if err := os.WriteFile(tmpFile, []byte(content), 0o600); err != nil {
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
