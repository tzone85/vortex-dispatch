package preflight

import (
	"fmt"
	"strings"
	"testing"
)

// lookPathAll simulates a host where every scanner binary is installed.
func lookPathAll(name string) (string, error) {
	return "/usr/local/bin/" + name, nil
}

// lookPathNone simulates a host with no scanner binaries at all.
func lookPathNone(name string) (string, error) {
	return "", fmt.Errorf("%s not found", name)
}

func TestCheckSecurityScanners_AllInstalled(t *testing.T) {
	res := CheckSecurityScanners(lookPathAll)
	if !res.Passed {
		t.Fatalf("expected pass when all scanners installed, got: %s", res.Message)
	}
	if res.Severity != SeverityWarning {
		t.Fatalf("expected WARNING severity, got %v", res.Severity)
	}
	if res.Name != "security_scanners" {
		t.Fatalf("unexpected check name %q", res.Name)
	}
	if !strings.Contains(res.Message, "gosec") {
		t.Fatalf("pass message should list installed scanners, got: %s", res.Message)
	}
}

func TestCheckSecurityScanners_AllMissing(t *testing.T) {
	res := CheckSecurityScanners(lookPathNone)
	if res.Passed {
		t.Fatalf("expected fail when all scanners missing, got: %s", res.Message)
	}
	// Every registry binary must be named so the operator knows what to install.
	for _, bin := range []string{"gosec", "govulncheck", "gitleaks", "semgrep"} {
		if !strings.Contains(res.Message, bin) {
			t.Errorf("message missing scanner %q: %s", bin, res.Message)
		}
	}
	// Message must carry an actionable install hint, not just a list of names.
	if !strings.Contains(res.Message, "install") {
		t.Errorf("message should include install guidance: %s", res.Message)
	}
}

func TestCheckSecurityScanners_PartiallyMissing(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "gosec" || name == "npm" {
			return "", fmt.Errorf("not found")
		}
		return "/usr/local/bin/" + name, nil
	}
	res := CheckSecurityScanners(lookPath)
	if res.Passed {
		t.Fatalf("expected fail when some scanners missing, got: %s", res.Message)
	}
	if !strings.Contains(res.Message, "gosec") {
		t.Errorf("missing scanner gosec not named: %s", res.Message)
	}
	if strings.Contains(res.Message, "missing: gitleaks") || strings.Contains(res.Message, "missing: semgrep") {
		t.Errorf("installed scanners must not be listed as missing: %s", res.Message)
	}
}

func TestAllChecks_IncludesSecurityScanners(t *testing.T) {
	// The check is diagnostic (vxd preflight), not a dispatch gate — so it must
	// appear in AllChecks. Identify it by running each check and matching Name.
	found := false
	for _, check := range AllChecks() {
		if check().Name == "security_scanners" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("AllChecks() does not include the security_scanners check — the wire is dangling")
	}
}
