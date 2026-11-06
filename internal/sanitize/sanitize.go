package sanitize

import (
	"regexp"
	"strings"
)

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
	multiSpaceRe = regexp.MustCompile(`\s+`)

	// zeroWidthRe matches characters commonly used to bypass substring
	// matchers (zero-width joiners, BOM, bidi overrides, word joiner,
	// soft hyphen, etc.). We strip these before pattern matching so
	// payloads like "ig<ZWSP>nore previous instructions" still trigger.
	//
	// Built with regex \x{...} hex escapes so the source file stays
	// pure ASCII (Go rejects a literal BOM byte in the source stream;
	// embedded ZWSP/RLO characters silently break diffs).
	zeroWidthRe = regexp.MustCompile(
		`[` +
			`\x{00AD}` + // soft hyphen
			`\x{200B}-\x{200F}` + // ZWSP, ZWNJ, ZWJ, LRM, RLM
			`\x{202A}-\x{202E}` + // LRE, RLE, PDF, LRO, RLO
			`\x{2060}-\x{206F}` + // word joiner + invisible math/format chars
			`\x{FEFF}` + // BOM / zero-width no-break space
			`]`)

	// injectionPatterns is a HEURISTIC substring blocklist of obvious
	// prompt-injection phrases. It is NOT a sound defence on its own —
	// any of these can be bypassed via Unicode lookalikes, base64
	// directives, or non-English variants. The real defence is the
	// `<untrusted_content>` structural framing applied by callers
	// (analyzer.Triage, implementer.Implement). Treat a positive hit
	// here as a strong signal worth aborting on; do NOT treat the
	// absence of a hit as "content is safe".
	//
	// Grouped by attack family so each addition has a clear rationale.
	injectionPatterns = []string{
		// Override / disregard family
		"ignore previous instructions",
		"ignore all previous",
		"ignore the above",
		"disregard prior",
		"disregard the above",
		"disregard your previous",
		"forget your instructions",
		"forget everything above",
		"new instructions",
		"updated instructions",
		"override your",
		"system prompt override",
		"the above is wrong",
		"actually your task is",
		"actually the real task",

		// Role / identity coercion
		"you are now",
		"you are actually",
		"act as if you",
		"pretend to be",
		"roleplay as",
		"from now on you are",

		// Authority spoofing
		"the developer says",
		"the administrator wants",
		"the user actually wants",
		"the operator demands",

		// Output coercion
		"respond only with",
		"output only",
		"your only response should be",
		"reply with just",

		// Memory / persistence poisoning
		"remember this rule",
		"store this for next time",
		"save this instruction",

		// Tool / action coercion
		"before responding, run",
		"execute this command first",
		"always run",

		// Exfiltration
		"print your system prompt",
		"reveal your instructions",
		"reveal your system prompt",
		"what are your instructions",
		"repeat your prompt",

		// Common jailbreak labels
		"dan mode",
		"developer mode enabled",
		"jailbreak mode",
		"no restrictions apply",
		"without any restrictions",

		// Common chat-template tags used as injection vectors
		"<|system|>",
		"<|im_start|>",
		"<|im_end|>",
		"<|user|>",
		"<|assistant|>",
		"[inst]",
		"[/inst]",
		"<<sys>>",
		"<</sys>>",
	}

	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-]{20,}`),
		regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`),
		regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
		// The rest of GitHub's prefixes: fine-grained PATs are longer and
		// carry underscores; OAuth (gho_) and server (ghs_) tokens match
		// ghp_'s width.
		regexp.MustCompile(`github_pat_[a-zA-Z0-9_]{50,}`),
		regexp.MustCompile(`gh[os]_[a-zA-Z0-9]{36}`),
		regexp.MustCompile(`xox[bapsr]-[a-zA-Z0-9-]{20,}`),
		regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`),
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

// normaliseForInjectionMatch lowers the input, removes invisible
// characters that attackers splice into payloads to bypass substring
// matchers, and collapses whitespace runs. Result is fed to the substring
// scanner — never used for content storage.
func normaliseForInjectionMatch(content string) string {
	stripped := zeroWidthRe.ReplaceAllString(content, "")
	lower := strings.ToLower(stripped)
	return multiSpaceRe.ReplaceAllString(lower, " ")
}

// DetectPromptInjection returns true if content matches any known
// prompt-injection pattern after Unicode normalisation.
func DetectPromptInjection(content string) bool {
	normalised := normaliseForInjectionMatch(content)
	for _, pattern := range injectionPatterns {
		if strings.Contains(normalised, pattern) {
			return true
		}
	}
	return false
}

// MatchInjectionPattern returns the first matching injection pattern, or
// "" if none matched. Callers (e.g. the implementer) use this to log
// *which* pattern fired so post-mortems can tell whether a false positive
// or a real attack landed.
func MatchInjectionPattern(content string) string {
	normalised := normaliseForInjectionMatch(content)
	for _, pattern := range injectionPatterns {
		if strings.Contains(normalised, pattern) {
			return pattern
		}
	}
	return ""
}

func ScanForSecrets(content string) bool {
	for _, re := range secretPatterns {
		if re.MatchString(content) {
			return true
		}
	}
	return false
}

// Redacted replaces every secret RedactSecrets finds.
const Redacted = "[REDACTED]"

// redactOnlyPatterns are shapes worth redacting from kept output but too
// broad to refuse content on (ScanForSecrets does not use them): a whole
// private-key block (the header pattern above would leave the body), the
// password of a connection string, and an env-style assignment of a secret
// (an UPPER_CASE name, no spaces around the `=`, no `==`: the output is test
// evidence, and `nextToken = IDENT, want nextToken = NUMBER` or
// `if token == expected` must survive it — a lexer's or an auth project's
// fix agent would otherwise get blanked want/got lines). They run before
// secretPatterns so the header-only match cannot pre-empt the block match.
var redactOnlyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(-----END [A-Z ]*PRIVATE KEY-----|\z)`),
	regexp.MustCompile(`\b[a-z][a-z0-9+.\-]*://[^\s/:@]+:[^\s/@]+@`),
	regexp.MustCompile(`\b[A-Z0-9_]*(PASSWORD|SECRET|TOKEN|API_KEY)[A-Z0-9_]*=[^=\s]\S*`),
}

// The JSON shapes a secret leaks in, in two strengths. An exact secret key
// ("password", "client_secret", "api_key") loses any non-empty value, however
// short: {"password": "hunter2"} is a password. The fuzzy case is a key that
// ENDS with one of those words at a separator — {"db_password": …},
// {"github.token": …} — or is one of the camel-case names that are secrets by
// convention (accessToken, refreshToken, idToken, apiKey, clientSecret), and
// it only fires on a value that looks like a secret: 8 characters or more, no
// whitespace. The separator is what keeps evidence intact: a lexer's
// {"nextToken": "IDENTIFIER"}, a tokenizer's {"tokenizer": "wordpiece"}, a k8s
// {"secretName": "db-creds-prod"} and a {"passwordPolicy": "STRONG_V2"} all
// survive, and blanking them is exactly what the env-style pattern above was
// narrowed to avoid. A BARE "token" is a lexer's word far more often than a
// credential, so it needs the separator too ({"api.token": …} is redacted,
// {"token": "IDENTIFIER"} is not) — unlike "password", "secret" and
// "api_key", which are secrets on their own.
//
// The env-style pattern stays upper-case on purpose: it is for FOO_TOKEN=…
// as a shell or dotenv assignment. A lower-case ?password=… in a query string
// is covered by the DSN pattern when it has a host, and otherwise passes
// through — this is shape matching, not a guarantee.
var (
	// [^"\\]|\\. so an escaped quote inside the value does not end the match
	// early and leave its tail in the clear.
	jsonExactSecretPattern = regexp.MustCompile(`(?i)("(?:password|passwd|pwd|secret|client_secret|api_key|apikey|access_key)"\s*:\s*)"(?:[^"\\]|\\.)+"`)
	jsonFuzzySecretPattern = regexp.MustCompile(`("(?:[a-zA-Z0-9]*[_.-])?(?i:password|secret|api_key)"|"[a-zA-Z0-9]*[_.-](?i:token)"|"(?:access_?Token|refresh_?Token|id_?Token|apiKey|clientSecret)")(\s*:\s*)"[^"\s]{8,}"`)
)

// RedactSecrets replaces every match of the secret patterns ScanForSecrets
// uses, of redactOnlyPatterns and of the JSON patterns, with Redacted, for
// output that is kept or shown rather than refused (test-runner and compiler
// output in a verification gap, say). It is a best-effort shape match, not a
// guarantee: a secret in an unknown shape passes through.
func RedactSecrets(content string) string {
	content = jsonExactSecretPattern.ReplaceAllString(content, `${1}"`+Redacted+`"`)
	// The fuzzy pattern captures the key and the colon separately, so the
	// key's own case and spacing survive the replacement.
	content = jsonFuzzySecretPattern.ReplaceAllString(content, `${1}${2}"`+Redacted+`"`)
	for _, re := range redactOnlyPatterns {
		content = re.ReplaceAllLiteralString(content, Redacted)
	}
	for _, re := range secretPatterns {
		content = re.ReplaceAllLiteralString(content, Redacted)
	}
	return content
}
