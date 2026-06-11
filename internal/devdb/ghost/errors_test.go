package ghost

import (
	"strings"
	"testing"
)

func TestSafeBody_TruncatesLongResponses(t *testing.T) {
	long := strings.Repeat("x", 4096)
	got := safeBody([]byte(long))
	if len(got) > maxErrorBodyBytes {
		t.Errorf("safeBody = %d chars, want <= %d", len(got), maxErrorBodyBytes)
	}
}

func TestSafeBody_RedactsURLDSNPassword(t *testing.T) {
	in := `{"error":"connection failed for postgres://vxd:secret@db.ghost.build:5432/store"}`
	got := safeBody([]byte(in))
	if strings.Contains(got, "secret") {
		t.Errorf("safeBody leaked password: %q", got)
	}
	if !strings.Contains(got, ":***@") {
		t.Errorf("safeBody missing redaction marker: %q", got)
	}
}

func TestSafeBody_RedactsKeyValuePassword(t *testing.T) {
	in := "connect string: host=db user=vxd password=topsecret dbname=store"
	got := safeBody([]byte(in))
	if strings.Contains(got, "topsecret") {
		t.Errorf("safeBody leaked password: %q", got)
	}
	if !strings.Contains(got, "password=***") {
		t.Errorf("safeBody missing redaction marker: %q", got)
	}
}

func TestSafeBody_LeavesNonSensitiveContent(t *testing.T) {
	in := `{"error":"rate limit exceeded, retry after 60s"}`
	got := safeBody([]byte(in))
	if got != in {
		t.Errorf("safeBody changed benign body: %q != %q", got, in)
	}
}
