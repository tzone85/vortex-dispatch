package dashstart

import "runtime"

// Environ abstracts environment lookups so tests can inject values without
// poking the real process env. The single Getenv method matches os.Getenv.
type Environ interface {
	Getenv(key string) string
}

// OSEnv is the real-process Environ implementation.
type OSEnv struct{}

// Getenv delegates to os.Getenv. Kept as a method so OSEnv satisfies Environ.
func (OSEnv) Getenv(k string) string { return osGetenv(k) }

// IsHeadless reports whether the caller should skip opening a browser.
// Returns true plus a short reason string suitable for logging.
//
// The check uses, in order:
//
//  1. cfgAutoOpen=false   (operator switch)
//  2. BROWSER=none        (POSIX convention for "no browser")
//  3. SSH_TTY present     (running over SSH — no local display)
//  4. linux + empty $DISPLAY and empty $WAYLAND_DISPLAY (headless server)
//  5. isTTY=false         (piped or non-interactive shell)
//
// Callers pass isTTY themselves so this function stays pure and testable.
func IsHeadless(env Environ, isTTY bool, cfgAutoOpen bool) (bool, string) {
	if !cfgAutoOpen {
		return true, "dashboard.auto_open=false"
	}
	if env.Getenv("BROWSER") == "none" {
		return true, "BROWSER=none"
	}
	if env.Getenv("SSH_TTY") != "" || env.Getenv("SSH_CONNECTION") != "" {
		return true, "ssh session"
	}
	if runtime.GOOS == "linux" {
		if env.Getenv("DISPLAY") == "" && env.Getenv("WAYLAND_DISPLAY") == "" {
			return true, "no DISPLAY / WAYLAND_DISPLAY"
		}
	}
	if !isTTY {
		return true, "stdout not a tty"
	}
	return false, ""
}
