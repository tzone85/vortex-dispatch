package dashstart

import "testing"

// stubEnv lets tests inject specific env-var values without touching the
// real process environment.
type stubEnv map[string]string

func (s stubEnv) Getenv(k string) string { return s[k] }

func TestIsHeadless_AutoOpenOff(t *testing.T) {
	headless, reason := IsHeadless(stubEnv{}, true, false)
	if !headless {
		t.Fatal("auto_open=false must force headless")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestIsHeadless_BrowserNone(t *testing.T) {
	headless, reason := IsHeadless(stubEnv{"BROWSER": "none"}, true, true)
	if !headless || reason != "BROWSER=none" {
		t.Fatalf("got headless=%v reason=%q", headless, reason)
	}
}

func TestIsHeadless_SSHSession(t *testing.T) {
	for _, key := range []string{"SSH_TTY", "SSH_CONNECTION"} {
		t.Run(key, func(t *testing.T) {
			headless, reason := IsHeadless(stubEnv{key: "1"}, true, true)
			if !headless || reason != "ssh session" {
				t.Fatalf("got headless=%v reason=%q", headless, reason)
			}
		})
	}
}

func TestIsHeadless_NonTTY(t *testing.T) {
	// Force the non-linux paths by passing no DISPLAY-related env; on
	// linux the linux check fires first, on darwin/windows the non-tty
	// check fires. Both should yield headless=true.
	headless, _ := IsHeadless(stubEnv{}, false, true)
	if !headless {
		t.Fatal("non-tty stdout must force headless")
	}
}

func TestIsHeadless_FullyInteractive(t *testing.T) {
	// On non-linux platforms the linux DISPLAY check is skipped, so a
	// fully-populated env + tty + auto_open=true must yield NOT headless.
	env := stubEnv{
		"DISPLAY":         ":0",
		"WAYLAND_DISPLAY": "wayland-0",
	}
	headless, reason := IsHeadless(env, true, true)
	if headless {
		t.Fatalf("expected interactive, got headless reason=%q", reason)
	}
}
