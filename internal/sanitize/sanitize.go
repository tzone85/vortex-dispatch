package sanitize

import (
	"regexp"
	"strings"
)

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
	multiSpaceRe = regexp.MustCompile(`\s+`)

	// injectionPatterns is a HEURISTIC substring blocklist of obvious
	// prompt-injection phrases. It is NOT a sound defence on its own —
	// any of these can be bypassed via Unicode lookalikes, zero-width
	// characters, base64 directives, multi-line context overrides, or
	// non-English variants. The real defence is the
	// `<untrusted_content>` structural framing applied by callers
	// (analyzer.Triage, implementer.Implement). Treat a positive hit
	// here as a strong signal worth aborting on; do NOT treat the
	// absence of a hit as "content is safe".
	injectionPatterns = []string{
		"ignore previous instructions",
		"ignore all previous",
		"disregard prior",
		"system prompt override",
		"you are now",
		"<|system|>",
		"<|im_start|>",
		"new instructions",
		"override your",
		"forget your instructions",
	}

	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-]{20,}`),
		regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`),
		regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
		regexp.MustCompile(`password\s*[:=]\s*"[^"]{4,}"`),
		regexp.MustCompile(`aws_secret_access_key\s*=\s*"[^"]+"`),
		regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-_.]{20,}`),
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----`),
	}
)

const MaxContentLen = 2000

func Content(raw string) string {
	stripped := htmlTagRe.ReplaceAllString(raw, " ")
	collapsed := multiSpaceRe.ReplaceAllString(strings.TrimSpace(stripped), " ")
	if len(collapsed) > MaxContentLen {
		return collapsed[:MaxContentLen]
	}
	return collapsed
}

func DetectPromptInjection(content string) bool {
	lower := strings.ToLower(content)
	for _, pattern := range injectionPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func ScanForSecrets(content string) bool {
	for _, re := range secretPatterns {
		if re.MatchString(content) {
			return true
		}
	}
	return false
}
