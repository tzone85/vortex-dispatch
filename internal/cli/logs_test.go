package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReqLogPath_Composition(t *testing.T) {
	got := reqLogPath("/home/u/.vxd/projects/mycli", "REQ-123")
	want := filepath.Join("/home/u/.vxd/projects/mycli", "logs", "req-REQ-123.log")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReqLogPath_EmptyReqID(t *testing.T) {
	got := reqLogPath("/tmp/p", "")
	if !strings.HasSuffix(got, "logs/req-.log") {
		t.Errorf("unexpected path for empty req-id: %q", got)
	}
}

func TestReqLogPath_EmptyDir(t *testing.T) {
	got := reqLogPath("", "REQ-X")
	if got != "logs/req-REQ-X.log" {
		t.Errorf("got %q, want logs/req-REQ-X.log", got)
	}
}
