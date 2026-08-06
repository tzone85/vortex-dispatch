package web

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
)

const quietHorizonVideoURL = "https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260428_193507_4286c423-2fd9-4efd-92bd-91a939453fc1.mp4"

func readStaticAsset(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(staticFiles, "static/"+name)
	if err != nil {
		t.Fatalf("read embedded %s: %v", name, err)
	}
	return b
}

func TestDashboardQuietHorizonWiring(t *testing.T) {
	html := string(readStaticAsset(t, "index.html"))
	for _, want := range []string{
		`class="dashboard-background"`,
		`aria-hidden="true"`,
		`class="dashboard-background__video"`,
		"autoplay muted loop playsinline",
		`preload="metadata"`,
		quietHorizonVideoURL,
		`class="dashboard-background__horizon"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing Quiet Horizon wiring %q", want)
		}
	}

	css := string(readStaticAsset(t, "styles.css"))
	for _, want := range []string{
		".dashboard-background {",
		".dashboard-background__video {",
		".dashboard-background__horizon {",
		"@media (prefers-reduced-motion: reduce)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("styles.css missing Quiet Horizon rule %q", want)
		}
	}
}

func TestDashboardStaticSourcesUseLF(t *testing.T) {
	for _, name := range []string{"index.html", "styles.css"} {
		if content := readStaticAsset(t, name); bytes.Contains(content, []byte{'\r'}) {
			t.Errorf("static/%s contains CRLF or stray carriage returns", name)
		}
	}
}
