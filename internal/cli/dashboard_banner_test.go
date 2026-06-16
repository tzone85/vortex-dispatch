package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/dashstart"
)

// stubStoresForBanner returns a stores value with just the bits
// printDashboardBanner reads (Config + ProjectDir). Defined here so the test
// doesn't need real event/projection stores.
func stubStoresForBanner(t *testing.T) stores {
	t.Helper()
	return stores{
		Config: config.Config{
			Dashboard: config.DashboardConfig{
				AutoStart: true,
				AutoOpen:  false, // we don't want the test to fork a browser
				Port:      8787,
			},
		},
		ProjectDir: t.TempDir(),
	}
}

func TestPrintDashboardBanner_CallsEnsureAndPrintsURL(t *testing.T) {
	// Capture calls to ensure + browser-open.
	var ensureCalled bool
	var openedURL string
	prevEnsure := ensureDashboardFunc
	prevOpen := openBrowserFunc
	defer func() {
		ensureDashboardFunc = prevEnsure
		openBrowserFunc = prevOpen
	}()
	ensureDashboardFunc = func(_ context.Context, cfg dashstart.Config) (dashstart.Handle, error) {
		ensureCalled = true
		return dashstart.Handle{
			PID:            4242,
			Port:           cfg.Port,
			BootstrapNonce: "test-nonce",
			URL:            "http://localhost:8787",
			Reused:         false,
		}, nil
	}
	openBrowserFunc = func(url string) error {
		openedURL = url
		return nil
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	printDashboardBanner(cmd, stubStoresForBanner(t), "REQ12345678")

	if !ensureCalled {
		t.Fatal("ensureDashboardFunc was not called")
	}
	out := buf.String()
	if !strings.Contains(out, "Dashboard:") {
		t.Errorf("banner missing 'Dashboard:' line, got: %q", out)
	}
	if !strings.Contains(out, "?req=REQ12345678&bootstrap=test-nonce") {
		t.Errorf("URL missing req+bootstrap query, got: %q", out)
	}
	// auto_open=false → headless → browser must NOT open.
	if openedURL != "" {
		t.Errorf("auto_open=false but openBrowser was called with %q", openedURL)
	}
}

func TestPrintDashboardBanner_FailureDoesNotPanic(t *testing.T) {
	prevEnsure := ensureDashboardFunc
	defer func() { ensureDashboardFunc = prevEnsure }()
	ensureDashboardFunc = func(_ context.Context, _ dashstart.Config) (dashstart.Handle, error) {
		return dashstart.Handle{}, errors.New("simulated boot failure")
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Must NOT panic and must NOT block — the contract is "auto-spawn never
	// blocks dispatch".
	printDashboardBanner(cmd, stubStoresForBanner(t), "REQ12345678")

	out := buf.String()
	if !strings.Contains(out, "Dashboard: unavailable") {
		t.Errorf("expected 'unavailable' notice in output, got: %q", out)
	}
}

func TestDashboardURLForReq_OmitsBootstrapWhenEmpty(t *testing.T) {
	h := dashstart.Handle{URL: "http://localhost:8787"}
	url := dashboardURLForReq(h, "ABC")
	if url != "http://localhost:8787/?req=ABC" {
		t.Errorf("got %q", url)
	}
}

func TestDashboardURLForReq_IncludesBootstrap(t *testing.T) {
	h := dashstart.Handle{URL: "http://localhost:8787", BootstrapNonce: "n0nce"}
	url := dashboardURLForReq(h, "ABC")
	if url != "http://localhost:8787/?req=ABC&bootstrap=n0nce" {
		t.Errorf("got %q", url)
	}
}
