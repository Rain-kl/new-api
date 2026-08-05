package codexcompat

import (
	"regexp"
	"strings"
)

// DefaultClientName is the official Codex CLI originator / UA client segment
// (codex-rs DEFAULT_ORIGINATOR).
const DefaultClientName = "codex_cli_rs"

// Official UA / originator lists aligned with sub2api backend/internal/pkg/openai/request.go
// (codex_cli_only gate: strict HasPrefix for UA prefixes; exact originator set + "Codex " family).

// officialClientUAPrefixes are official Codex client User-Agent prefixes (each ends with '/').
// Matching is case-insensitive and strict (HasPrefix only; no substring Contains fallback).
var officialClientUAPrefixes = []string{
	"codex_cli_rs/",
	"codex-tui/",
	"codex_vscode/",
	"codex_vscode_copilot/",
	"codex_app/",
	"codex_chatgpt_desktop/",
	"codex_atlas/",
	"codex_exec/",
	"codex_sdk_ts/",
}

// officialClientFamilyPrefix covers "Codex Desktop" and other "Codex " first-party
// originators (codex-rs is_first_party_originator starts_with("Codex ")). Trailing space
// is intentional; matching uses the lowercased/trimmed value so it stays "codex ".
const officialClientFamilyPrefix = "codex "

// officialClientOriginators is the exact official originator set (matched after lower+trim).
var officialClientOriginators = map[string]bool{
	"codex_cli_rs":          true,
	"codex-tui":             true,
	"codex_vscode":          true,
	"codex_vscode_copilot":  true,
	"codex_app":             true,
	"codex_chatgpt_desktop": true,
	"codex_atlas":           true,
	"codex_exec":            true,
	"codex_sdk_ts":          true,
}

// engineVersionPattern extracts the leading X.Y.Z triple from a UA version segment
// (drops -alpha / other pre-release suffixes).
var engineVersionPattern = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

// ParseEngineVersion extracts the codex-rs engine version X.Y.Z from a User-Agent of the
// form `{client}/{X.Y.Z}...` (first '/' then first space or '('). Optional pre-release
// suffixes after the triple are ignored for the returned version string.
func ParseEngineVersion(userAgent string) (version string, ok bool) {
	ua := strings.TrimSpace(userAgent)
	slash := strings.IndexByte(ua, '/')
	if slash < 0 {
		return "", false
	}
	rest := ua[slash+1:]
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		if rest[i] == ' ' || rest[i] == '(' {
			end = i
			break
		}
	}
	m := engineVersionPattern.FindString(strings.TrimSpace(rest[:end]))
	if m == "" {
		return "", false
	}
	return m, true
}

// IsOfficialUserAgentStrict reports whether userAgent is an official Codex client UA
// under the strict codex_cli_only rules: official prefix list via HasPrefix only (no
// Contains fallback), "Codex " family prefix, or a trailing `(name; version)` group whose
// name is an official originator (CODEX_INTERNAL_ORIGINATOR_OVERRIDE recovery).
func IsOfficialUserAgentStrict(userAgent string) bool {
	ua := normalizeHeader(userAgent)
	if ua == "" {
		return false
	}
	for _, prefix := range officialClientUAPrefixes {
		p := normalizeHeader(prefix)
		if p != "" && strings.HasPrefix(ua, p) {
			return true
		}
	}
	if strings.HasPrefix(ua, officialClientFamilyPrefix) {
		return true
	}
	// Trailer fallback: last parenthesized `(name; version)` may recover overridden prefix.
	if name := uaTrailerName(ua); name != "" {
		return IsOfficialOriginator(name)
	}
	return false
}

// IsOfficialOriginator reports whether originator is an official Codex client identity:
// exact set match (case-insensitive) or "Codex " family prefix.
func IsOfficialOriginator(originator string) bool {
	v := normalizeHeader(originator)
	if v == "" {
		return false
	}
	if officialClientOriginators[v] {
		return true
	}
	return strings.HasPrefix(v, officialClientFamilyPrefix)
}

// IsGateCapableOfficialCodex is true only when the inbound identity is suitable for the
// preserve path: (strict official UA or official originator) and a parseable engine
// version X.Y.Z in the User-Agent. Originator alone without a versioned UA is not enough.
func IsGateCapableOfficialCodex(userAgent, originator string) bool {
	if !IsOfficialUserAgentStrict(userAgent) && !IsOfficialOriginator(originator) {
		return false
	}
	_, ok := ParseEngineVersion(userAgent)
	return ok
}

func normalizeHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// uaTrailerName extracts clientInfo.name from the last parenthesized group of a codex-rs
// User-Agent: `{orig}/{ver} ({os}; {arch}) {term} ({name}; {ver})`.
func uaTrailerName(ua string) string {
	last := strings.LastIndex(ua, "(")
	if last < 0 {
		return ""
	}
	rest := ua[last+1:]
	closeIdx := strings.Index(rest, ")")
	if closeIdx < 0 {
		return ""
	}
	inner := strings.TrimSpace(rest[:closeIdx])
	if semi := strings.Index(inner, ";"); semi >= 0 {
		inner = strings.TrimSpace(inner[:semi])
	}
	return inner
}
