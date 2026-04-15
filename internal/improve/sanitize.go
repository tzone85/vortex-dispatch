package improve

import "github.com/tzone85/vortex-dispatch/internal/sanitize"

// SanitizeContent strips HTML, collapses whitespace, and truncates.
func SanitizeContent(raw string) string {
	return sanitize.Content(raw)
}

// DetectPromptInjection checks content for known prompt injection patterns.
func DetectPromptInjection(content string) bool {
	return sanitize.DetectPromptInjection(content)
}

// ScanForSecrets checks a diff or code block for hardcoded secrets.
func ScanForSecrets(content string) bool {
	return sanitize.ScanForSecrets(content)
}
