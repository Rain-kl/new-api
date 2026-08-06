package advancedcustom

import (
	"net/http"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

const (
	// claudeCodeUserAgent is the official Claude Code CLI User-Agent (mirrors cc-switch).
	claudeCodeUserAgent = "claude-cli/1.0.119 (external, cli)"
	// claudeCodeBeta is the Anthropic beta header Claude Code sends.
	claudeCodeBeta = "claude-code-20250219"
	// claudeCodeSystemIdentity is the Claude Code identity line injected as the
	// first system block (Anthropic subscription/OAuth plans require it).
	claudeCodeSystemIdentity = "You are Claude Code, Anthropic's official CLI for Claude."
	// defaultCodexClientVersion is used when codex_compat is enabled but the
	// stored version is empty (defensive; validation normally requires it).
	defaultCodexClientVersion = "0.146.0"
)

// applyCodexIdentityHeaders shapes outbound requests like the official Codex CLI
// when the "Simulate Codex client" channel setting is enabled. It sets
// User-Agent/originator and passes through client-provided conversation headers
// (session_id, thread_id, x-codex-*), with names canonicalized by http.Header.
// It never synthesizes IDs.
func applyCodexIdentityHeaders(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) {
	if header == nil || info == nil {
		return
	}
	name := strings.TrimSpace(info.ChannelSetting.CodexClientName)
	if name == "" {
		name = dto.DefaultCodexClientName
	}
	version := strings.TrimSpace(info.ChannelSetting.CodexClientVersion)
	if version == "" {
		version = defaultCodexClientVersion
	}
	header.Set("User-Agent", name+"/"+version+" (linux; x86_64)")
	header.Set("originator", name)

	var inbound http.Header
	if c != nil && c.Request != nil {
		inbound = c.Request.Header
	}
	for _, h := range []string{"session_id", "thread_id"} {
		if v := headerGetAny(inbound, h); v != "" {
			header.Set(h, v)
		}
	}
	for k, vals := range inbound {
		if !strings.HasPrefix(strings.ToLower(k), "x-codex-") {
			continue
		}
		canon := strings.ToLower(k)
		header.Del(canon)
		for _, v := range vals {
			header.Add(canon, v)
		}
	}
}

// applyClaudeCodeHeaders applies the Claude Code client fingerprint when the
// "Simulate Claude Code client" channel setting is enabled and the route targets
// Claude Messages.
func applyClaudeCodeHeaders(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) {
	if header == nil || info == nil {
		return
	}
	header.Set("User-Agent", claudeCodeUserAgent)
	header.Set("x-app", "cli")

	beta := ""
	if c != nil && c.Request != nil {
		beta = c.Request.Header.Get("anthropic-beta")
	}
	beta = strings.TrimSpace(beta)
	if beta == "" {
		beta = claudeCodeBeta
	} else if !strings.Contains(beta, claudeCodeBeta) {
		beta = claudeCodeBeta + "," + beta
	}
	header.Set("anthropic-beta", beta)
}

// prependClaudeCodeSystemPrompt injects the Claude Code identity line as the
// first system block. Idempotent: if the first block already equals the identity
// line the request is left unchanged.
func prependClaudeCodeSystemPrompt(request *dto.ClaudeRequest) {
	if request == nil {
		return
	}
	identity := dto.ClaudeMediaMessage{Type: "text"}
	identity.SetText(claudeCodeSystemIdentity)

	switch system := request.System.(type) {
	case string:
		blocks := []dto.ClaudeMediaMessage{identity}
		if text := strings.TrimSpace(system); text != "" {
			orig := dto.ClaudeMediaMessage{Type: "text"}
			orig.SetText(system)
			blocks = append(blocks, orig)
		}
		request.System = blocks
	case []dto.ClaudeMediaMessage:
		if len(system) == 0 {
			request.System = []dto.ClaudeMediaMessage{identity}
			return
		}
		if first := system[0].GetText(); first == claudeCodeSystemIdentity {
			return
		}
		request.System = append([]dto.ClaudeMediaMessage{identity}, system...)
	case nil:
		request.System = []dto.ClaudeMediaMessage{identity}
	}
}

// headerGetAny returns the first non-empty value among candidate header names,
// scanning raw keys for underscore forms that http.Header.Get may not canonicalize.
func headerGetAny(h http.Header, names ...string) string {
	if h == nil {
		return ""
	}
	for _, name := range names {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return v
		}
	}
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
