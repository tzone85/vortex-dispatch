package dashstart

import (
	"strings"
	"testing"
)

func TestBuildCmd_RequiredFields(t *testing.T) {
	cases := []struct {
		name string
		args SpawnArgs
		want string
	}{
		{"missing self", SpawnArgs{Port: 8787, Pidfile: "/a", BootstrapFile: "/b"}, "Self path is required"},
		{"missing port", SpawnArgs{Self: "/vxd", Pidfile: "/a", BootstrapFile: "/b"}, "Port must be > 0"},
		{"missing pidfile", SpawnArgs{Self: "/vxd", Port: 8787, BootstrapFile: "/b"}, "Pidfile is required"},
		{"missing bootstrap", SpawnArgs{Self: "/vxd", Port: 8787, Pidfile: "/a"}, "BootstrapFile is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildCmd(tc.args)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestBuildCmd_HappyPath(t *testing.T) {
	cmd, err := BuildCmd(SpawnArgs{
		Self:          "/vxd",
		Port:          8787,
		Pidfile:       "/tmp/vxd.pid",
		BootstrapFile: "/tmp/vxd.bootstrap",
		NoOpen:        true,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := strings.Join(cmd.Args, " ")
	wantSubstrings := []string{
		"/vxd", "dashboard", "--web",
		"--port=8787",
		"--pidfile=/tmp/vxd.pid",
		"--bootstrap-file=/tmp/vxd.bootstrap",
		"--no-open",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
}

func TestFilteredEnv_StripsSensitiveKeys(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("VXD_KEEP_THIS", "keep")

	env := FilteredEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			t.Errorf("ANTHROPIC_API_KEY leaked into filtered env: %q", kv)
		}
		if strings.HasPrefix(kv, "CLAUDECODE=") {
			t.Errorf("CLAUDECODE leaked into filtered env: %q", kv)
		}
	}

	found := false
	for _, kv := range env {
		if kv == "VXD_KEEP_THIS=keep" {
			found = true
			break
		}
	}
	if !found {
		t.Error("FilteredEnv dropped a benign key VXD_KEEP_THIS")
	}
}
