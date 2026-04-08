package improve

import (
	"regexp"
	"strings"
)

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
	multiSpaceRe = regexp.MustCompile(`\s+`)
	maxContentLen = 2000

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

// SanitizeContent strips HTML, collapses whitespace, and truncates to maxContentLen.
func SanitizeContent(raw string) string {
	stripped := htmlTagRe.ReplaceAllString(raw, " ")
	collapsed := multiSpaceRe.ReplaceAllString(strings.TrimSpace(stripped), " ")
	if len(collapsed) > maxContentLen {
		return collapsed[:maxContentLen]
	}
	return collapsed
}

// DetectPromptInjection checks content for known prompt injection patterns.
func DetectPromptInjection(content string) bool {
	lower := strings.ToLower(content)
	for _, pattern := range injectionPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ScanForSecrets checks a diff or code block for hardcoded secrets.
func ScanForSecrets(content string) bool {
	for _, re := range secretPatterns {
		if re.MatchString(content) {
			return true
		}
	}
	return false
}
