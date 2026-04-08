package improve_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestCheckDiffSize_PassesUnderLimit(t *testing.T) {
	diff := "+line1\n+line2\n+line3\n-old1\n-old2\n"
	if err := improve.CheckDiffSize(diff, 500); err != nil {
		t.Errorf("expected pass for 5 lines, got: %v", err)
	}
}

func TestCheckDiffSize_FailsOverLimit(t *testing.T) {
	var diff string
	for i := 0; i < 600; i++ {
		diff += "+new line\n"
	}
	if err := improve.CheckDiffSize(diff, 500); err == nil {
		t.Error("expected error for 600+ changed lines")
	}
}

func TestCheckFileCount_PassesUnderLimit(t *testing.T) {
	stat := "5 files changed, 100 insertions(+), 20 deletions(-)"
	if err := improve.CheckFileCount(stat, 10); err != nil {
		t.Errorf("expected pass for 5 files, got: %v", err)
	}
}

func TestCheckFileCount_FailsOverLimit(t *testing.T) {
	stat := "15 files changed, 500 insertions(+)"
	if err := improve.CheckFileCount(stat, 10); err == nil {
		t.Error("expected error for 15 files")
	}
}

func TestCheckSecrets_PassesCleanDiff(t *testing.T) {
	diff := "+func NewClient(apiKey string) *Client {\n+    return &Client{key: apiKey}\n+}"
	if err := improve.CheckSecrets(diff); err != nil {
		t.Errorf("expected pass for clean diff, got: %v", err)
	}
}

func TestCheckSecrets_FailsWithSecret(t *testing.T) {
	diff := `+apiKey := "sk-ant-api03-real-secret-key-here-abcdef123456"`
	if err := improve.CheckSecrets(diff); err == nil {
		t.Error("expected error for diff containing secret")
	}
}

func TestImplementResult_Dispositions(t *testing.T) {
	r := improve.ImplementResult{Disposition: "implemented"}
	if !r.IsImplemented() {
		t.Error("expected IsImplemented true")
	}

	r2 := improve.ImplementResult{Disposition: "proposed"}
	if r2.IsImplemented() {
		t.Error("expected IsImplemented false for proposed")
	}
}
