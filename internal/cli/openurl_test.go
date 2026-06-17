package cli

import "testing"

// TestSafeBrowserURL guards openURL against cmd.exe command injection on
// Windows (where the URL is handed to a process that parses metacharacters)
// and against malformed/non-http schemes everywhere. A scheme/host check alone
// is insufficient: a valid https URL can still carry `&`, `%`, `^`, etc.
func TestSafeBrowserURL(t *testing.T) {
	reject := []string{
		// cmd.exe metacharacters inside an otherwise-valid https URL.
		"https://github.com/x?a=1&calc.exe",
		"https://github.com/x^&whoami",
		"https://github.com/x%PATH%",
		`https://github.com/x"`,
		"https://github.com/a\nb",
		"https://github.com/a|b",
		"https://github.com/a>b",
		// Non-http schemes / malformed.
		"file:///etc/passwd",
		"javascript:alert(1)",
		"ftp://example.com",
		"not a url",
		"",
	}
	for _, in := range reject {
		if out, ok := safeBrowserURL(in); ok {
			t.Errorf("safeBrowserURL(%q) = (%q, true), want rejected", in, out)
		}
	}

	accept := map[string]string{
		"https://github.com/tzone85/vortex-dispatch/pull/76": "https://github.com/tzone85/vortex-dispatch/pull/76",
		"http://localhost:8787/dashboard":                    "http://localhost:8787/dashboard",
	}
	for in, want := range accept {
		out, ok := safeBrowserURL(in)
		if !ok {
			t.Errorf("safeBrowserURL(%q) rejected a valid URL", in)
			continue
		}
		if out != want {
			t.Errorf("safeBrowserURL(%q) = %q, want %q", in, out, want)
		}
	}
}
