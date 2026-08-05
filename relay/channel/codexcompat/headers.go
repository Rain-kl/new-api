package codexcompat

import (
	"net/http"
	"strings"
)

// ApplyInput configures ApplyIdentityHeaders.
type ApplyInput struct {
	Mode          IdentityMode
	ClientVersion string
	ClientName    string
	Sticky        StickyIDs
	Inbound       http.Header
	GateCapable   bool
}

// BuildSyntheticUserAgent returns a minimal official-shaped UA that parses as X.Y.Z.
// OS/arch is static for determinism (design §8.4).
func BuildSyntheticUserAgent(clientName, version string) string {
	name := strings.TrimSpace(clientName)
	if name == "" {
		name = DefaultClientName
	}
	ver := strings.TrimSpace(version)
	return name + "/" + ver + " (linux; x86_64)"
}

// HasInboundCodexFingerprint reports whether any header name has the x-codex- prefix
// (case-insensitive). Used to decide whether preserve path must inject window-id.
func HasInboundCodexFingerprint(h http.Header) bool {
	if h == nil {
		return false
	}
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "x-codex-") {
			return true
		}
	}
	return false
}

// ApplyIdentityHeaders mutates dst with preserve or synthesize Codex identity headers.
//
//	if mode==synthesize OR (mode==auto && !gateCapable):
//	  synthetic UA, originator, session/thread/window from Sticky
//	else: // preserve (passthrough, or auto && gateCapable)
//	  copy inbound UA, originator, session, thread, all x-codex-*
//	  if !HasInboundCodexFingerprint: set x-codex-window-id from sticky
func ApplyIdentityHeaders(dst *http.Header, in ApplyInput) {
	if dst == nil {
		return
	}
	if *dst == nil {
		*dst = make(http.Header)
	}

	if shouldSynthesize(in.Mode, in.GateCapable) {
		applySynthesizeHeaders(*dst, in)
		return
	}
	applyPreserveHeaders(*dst, in)
}

func shouldSynthesize(mode IdentityMode, gateCapable bool) bool {
	switch mode {
	case IdentityModeSynthesize:
		return true
	case IdentityModePassthrough:
		return false
	case IdentityModeAuto, "":
		return !gateCapable
	default:
		// Unknown mode: treat as auto.
		return !gateCapable
	}
}

func applySynthesizeHeaders(dst http.Header, in ApplyInput) {
	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = DefaultClientName
	}
	dst.Set("User-Agent", BuildSyntheticUserAgent(name, in.ClientVersion))
	dst.Set("originator", name)
	// Always set sticky session/thread/window on synthesize path.
	if in.Sticky.SessionID != "" {
		dst.Set("session_id", in.Sticky.SessionID)
	}
	if in.Sticky.ThreadID != "" {
		dst.Set("thread_id", in.Sticky.ThreadID)
	}
	if in.Sticky.WindowID != "" {
		dst.Set("x-codex-window-id", in.Sticky.WindowID)
	}
}

func applyPreserveHeaders(dst http.Header, in ApplyInput) {
	if in.Inbound == nil {
		// Still inject fingerprint window if nothing inbound.
		if in.Sticky.WindowID != "" {
			dst.Set("x-codex-window-id", in.Sticky.WindowID)
		}
		return
	}

	if ua := headerGetAny(in.Inbound, "User-Agent", "user-agent"); ua != "" {
		dst.Set("User-Agent", ua)
	}
	if originator := headerGetAny(in.Inbound, "originator", "Originator"); originator != "" {
		dst.Set("originator", originator)
	}
	if session := headerGetAny(in.Inbound, "session_id", "session-id", "Session-Id"); session != "" {
		dst.Set("session_id", session)
	}
	if thread := headerGetAny(in.Inbound, "thread_id", "thread-id", "Thread-Id"); thread != "" {
		dst.Set("thread_id", thread)
	}

	// Copy all inbound x-codex-* headers (preserve client engine fingerprint).
	for k, vals := range in.Inbound {
		if !strings.HasPrefix(strings.ToLower(k), "x-codex-") {
			continue
		}
		// Canonicalize to lower-case hyphen form as codex-rs uses.
		canon := strings.ToLower(k)
		dst.Del(canon)
		for _, v := range vals {
			dst.Add(canon, v)
		}
	}

	if !HasInboundCodexFingerprint(in.Inbound) && in.Sticky.WindowID != "" {
		dst.Set("x-codex-window-id", in.Sticky.WindowID)
	}
}

// headerGetAny returns the first non-empty value among candidate header names.
func headerGetAny(h http.Header, names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return v
		}
	}
	// http.Header.Get is case-insensitive for canonical MIME keys; also scan raw keys
	// for underscore forms that may not canonicalize (session_id).
	for _, name := range names {
		want := strings.ToLower(name)
		for k, vals := range h {
			if strings.ToLower(k) != want {
				continue
			}
			for _, v := range vals {
				if t := strings.TrimSpace(v); t != "" {
					return t
				}
			}
		}
	}
	return ""
}
