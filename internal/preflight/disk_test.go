package preflight

import (
	"errors"
	"strings"
	"testing"
)

func TestEvaluateDiskSpace(t *testing.T) {
	cases := []struct {
		name       string
		free       uint64
		err        error
		wantPassed bool
		wantSubstr string
	}{
		{
			name:       "statfs error fails with reason",
			err:        errors.New("no such device"),
			wantPassed: false,
			wantSubstr: "no such device",
		},
		{
			name:       "below threshold warns about event log",
			free:       512 << 20, // 512 MiB
			wantPassed: false,
			wantSubstr: "Low disk space",
		},
		{
			name:       "above threshold passes",
			free:       20 << 30, // 20 GiB
			wantPassed: true,
			wantSubstr: "Disk space OK",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := evaluateDiskSpace("/probe/path", tc.free, tc.err)
			if r.Name != "disk_space" {
				t.Errorf("Name = %q, want disk_space", r.Name)
			}
			if r.Severity != SeverityWarning {
				t.Errorf("Severity = %v, want SeverityWarning", r.Severity)
			}
			if r.Passed != tc.wantPassed {
				t.Errorf("Passed = %v, want %v (message: %s)", r.Passed, tc.wantPassed, r.Message)
			}
			if !strings.Contains(r.Message, tc.wantSubstr) {
				t.Errorf("Message %q does not contain %q", r.Message, tc.wantSubstr)
			}
		})
	}
}

// TestCheckDiskSpace_RunsAgainstRealFilesystem smoke-tests the platform
// freeDiskBytes implementation: it must resolve a probe path that exists and
// produce a well-formed result (pass/fail depends on the host's actual free
// space, so only the shape is asserted).
func TestCheckDiskSpace_RunsAgainstRealFilesystem(t *testing.T) {
	r := CheckDiskSpace()
	if r.Name != "disk_space" {
		t.Errorf("Name = %q, want disk_space", r.Name)
	}
	if r.Message == "" {
		t.Error("Message must not be empty")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{512, "512 B"},
		{2 << 10, "2.0 KiB"},
		{512 << 20, "512.0 MiB"},
		{20 << 30, "20.0 GiB"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
