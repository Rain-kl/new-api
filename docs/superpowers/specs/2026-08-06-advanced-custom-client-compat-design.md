# Advanced Custom Channel Client Compatibility Design

**Date:** 2026-08-06
**Status:** Approved
**Scope:** new-api only (no sub2api code changes, no cc-switch code changes)
**Related prior work:** `docs/superpowers/specs/2026-08-05-codex-gateway-channel-design.md`, `docs/superpowers/plans/2026-08-05-sub2api-codex-compat.md`

## 1. Problem

The Sub2API channel (type 59) currently owns a Codex-compatibility layer
(`relay/channel/codexcompat/`) that was built before the operator discovered the
Advanced Custom channel (type 58). Advanced Custom already provides protocol
conversion via route converters (`openai_responses_to_claude_messages`,
`openai_chat_completions_to_openai_responses`, ...), so the Sub2API-specific
`chat_completions_to_responses` toggle and the Sub2API codex layer are redundant.

This round:

1. Adds two optional client-emulation toggles to the **Advanced Custom** channel:
   - **Simulate Codex client** (`codex_compat_enabled`) — spoof official Codex CLI
     identity headers for upstreams that gate on the Codex client family.
   - **Simulate Claude Code client** (`claude_compat_enabled`) — port the
     cc-switch "模拟 Claude Code 客户端" feature so Advanced Custom routes that
     target Claude Messages can pass "Claude Code only" gateway fingerprint checks.
2. **Removes** the Sub2API codex compatibility layer entirely; Advanced Custom
   becomes the single home for client emulation.
3. **Removes** the Sub2API `chat_completions_to_responses` per-channel toggle
   (reverts commit `b8a6b8ea`), keeping the pre-existing global
   `chat_completions_to_responses_policy`.

### Non-goals this round

- **No sticky ID synthesis.** The Sub2API sticky mechanism (deterministic
  session/thread/window UUID5 fallback, body `prompt_cache_key` injection) is NOT
  migrated. Advanced Custom relies entirely on its own conversion adapters for
  conversation state. A future round will refactor/re-add sticky prompt-cache
  support.
- No new channel type.
- No changes to cc-switch or sub2api upstream code.
- No TLS/JA3 impersonation, no auto-fetch of client versions.

## 2. Behavior

### 2.1 Simulate Codex client (`codex_compat_enabled`)

When enabled on an Advanced Custom channel, every outbound request on that
channel is shaped like the official Codex CLI:

- `User-Agent: {client_name}/{version} (linux; x86_64)`
  - `client_name` defaults to `codex_cli_rs` (configurable via
    `codex_client_name`).
  - `version` comes from `codex_client_version`; required when enabled and must
    start with `X.Y.Z`.
- `originator: {client_name}` (default `codex_cli_rs`).
- **Passthrough only** of inbound conversation headers the real client already
  sent: `session_id`, `thread_id`, and every `x-codex-*` header. No synthesis of
  missing IDs.

Default off. When off, no behavior change.

### 2.2 Simulate Claude Code client (`claude_compat_enabled`)

When enabled on an Advanced Custom channel, and the resolved route targets
Claude Messages (converter `openai_chat_completions_to_anthropic_messages`,
`openai_responses_to_claude_messages`, or native Claude passthrough), the
outbound request is shaped like the official Claude Code CLI:

- `User-Agent: claude-cli/1.0.119 (external, cli)` (fixed, mirrors cc-switch).
- `anthropic-beta`: ensure `claude-code-20250219` is present (prepend if the
  inbound value is missing it; keep other beta values).
- `x-app: cli`.
- `anthropic-version`: already handled by the existing `applyClaudeHeaders`
  (defaults to `2023-06-01` when the client sent none).
- **System prompt identity**: prepend
  `You are Claude Code, Anthropic's official CLI for Claude.` as the first
  system block. Idempotent: if the first system block already equals the
  identity line, leave the request unchanged. Normalizes `system` string →
  `[{type:text, text: identity}, {type:text, text: original}]`.

Default off. When off, no behavior change.

## 3. Architecture

```text
Client (Codex CLI / Claude Code / any OpenAI-compatible client)
        │
        ▼
new-api ChannelTypeAdvancedCustom (58)
  route match → converter → outbound body (converted request)
  SetupRequestHeader:
    - simulate codex client (on): UA + originator + passthrough x-codex-*
    - simulate claude code client (on) + Claude-targeted route:
      claude-cli UA + anthropic-beta claude-code + x-app: cli
  convert path (Claude-targeted + claude compat on):
    prepend Claude Code identity to ClaudeRequest.System
        │
        ▼
Upstream (sub2api codex_cli_only gateway / Claude Code-only gateway / ...)
```

## 4. Backend changes

### 4.1 `relaykit/dto/channel_settings.go`

- Keep `CodexCompatEnabled`, `CodexClientVersion`, `CodexClientName`.
- **Remove** `CodexIdentityMode` and its constants
  (`CodexIdentityModeAuto/Passthrough/Synthesize`).
- **Add** `ClaudeCompatEnabled bool json:"claude_compat_enabled,omitempty"`.
- Simplify `ValidateCodexCompat`: when enabled, `codex_client_version` is
  required and must match `^\d+\.\d+\.\d+`; drop mode validation.
- `codexClientVersionPattern` stays.

### 4.2 `relay/channel/advancedcustom/compat.go` (new)

Pure helpers in package `advancedcustom`:

- `applyCodexIdentityHeaders(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo)`
  - Sets UA + originator; copies inbound `session_id` / `thread_id` / `x-codex-*`
    when present. No ID synthesis.
- `applyClaudeCodeHeaders(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo)`
  - Sets claude-cli UA, ensures `anthropic-beta` contains `claude-code-20250219`,
    sets `x-app: cli`.
- `prependClaudeCodeSystemPrompt(request *dto.ClaudeRequest)`
  - Idempotent system identity injection (string → array; array first-block
    identity check).

Constants:

- `defaultCodexClientName = "codex_cli_rs"` (reuse existing
  `dto.DefaultCodexClientName` where sensible)
- `claudeCodeUserAgent = "claude-cli/1.0.119 (external, cli)"`
- `claudeCodeBeta = "claude-code-20250219"`
- `claudeCodeSystemIdentity = "You are Claude Code, Anthropic's official CLI for Claude."`

### 4.3 `relay/channel/advancedcustom/adaptor.go`

- `SetupRequestHeader`: after existing auth/claude header logic, call
  `applyCodexIdentityHeaders` (when `CodexCompatEnabled`) and
  `applyClaudeCodeHeaders` (when `ClaudeCompatEnabled` and route is
  Claude-targeted).
- Claude-targeted convert paths call `prependClaudeCodeSystemPrompt` on the
  resulting `*dto.ClaudeRequest` when `ClaudeCompatEnabled`:
  - `ConvertOpenAIRequest` case `ConverterOpenAIChatToClaudeMessages`
  - `ConvertOpenAIResponsesRequest` case `ConverterOpenAIResponsesToClaudeMessages`
  - `ConvertClaudeRequest` case `ConverterNone` (native Claude passthrough)

### 4.4 Removals

- `relay/channel/sub2api/adaptor.go`: revert to plain passthrough
  (`newapi.Adaptor` embedded + `GetModelList` + `GetChannelName`); delete
  `codexcompat`-related helpers and tests.
- Delete `relay/channel/codexcompat/` package entirely (8 source + 5 test files).
- `relay/claude_handler.go` / `relay/compatible_handler.go`: revert the
  `ShouldChatCompletionsUseResponses(..., channelForce)` calls back to
  `ShouldChatCompletionsUseResponsesGlobal(...)`.
- `service/openai_chat_responses_mode.go`: remove the
  `ShouldChatCompletionsUseResponses` channelForce variant (keep
  `ShouldChatCompletionsUseResponsesPolicy` / `ShouldChatCompletionsUseResponsesGlobal`).
- Frontend: remove `chat_completions_to_responses` and `codex_identity_mode`
  fields/UI; move codex compat UI to Advanced Custom; add Claude compat UI.

## 5. Frontend changes

### 5.1 `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`

- Move the codex compat section from the `CHANNEL_TYPE_SUB2API` block to the
  `CHANNEL_TYPE_ADVANCED_CUSTOM` block.
- Codex compat section: toggle labeled **"模拟 Codex 客户端"** (Simulate Codex
  client) + `codex_client_version` input + optional `codex_client_name` input.
  Remove the identity-mode select.
- New Claude compat section (Advanced Custom only): toggle labeled
  **"模拟 Claude Code 客户端"** (Simulate Claude Code client) bound to
  `claude_compat_enabled`.
- Remove the `chat_completions_to_responses` toggle.

### 5.2 Form / types

- `web/src/features/channels/lib/channel-form.ts`: drop
  `codex_identity_mode` / `chat_completions_to_responses`; add
  `claude_compat_enabled`; keep codex version/name (required version when
  enabled).
- `web/src/features/channels/types.ts`: sync channel settings type.
- `web/src/features/channels/lib/channel-form-errors.ts`: sync validation.

### 5.3 i18n

- Update all 7 locale files (`en`, `zh`, `zh-TW`, `fr`, `ru`, `ja`, `vi`) via
  the project i18n tooling (`bun run i18n:sync`), adding the new toggle labels /
  descriptions and removing keys for removed toggles. Follow the project
  i18n-translate skill.

## 6. Testing

- `relay/channel/advancedcustom/adaptor_test.go` (or a new `compat_test.go`):
  - codex compat on: UA/originator set, inbound `x-codex-*`/`session_id`/
    `thread_id` passthrough, no synthesized IDs when absent.
  - codex compat off: no headers touched.
  - claude compat on + Claude-targeted route: claude-cli UA, `anthropic-beta`
    contains `claude-code-20250219` (prepend when missing, keep when present),
    `x-app: cli`.
  - system identity prepend: string system → array; array already-identity →
    idempotent; non-Claude route → untouched.
- `relaykit/dto/channel_settings_test.go` / `channel_settings_codex_compat_test.go`:
  rewrite for the simplified `ValidateCodexCompat` (version required/pattern),
  drop mode cases.
- Delete `relay/channel/codexcompat/*_test.go`, sub2api codex tests, and
  channelForce tests in `service/openai_chat_responses_mode_test.go`.
- Verify `cd relaykit && GOWORK=off go build ./...` (relaykit independence) and
  root `go build ./...` + affected `go test`.

## 7. Naming

- JSON fields: keep `codex_compat_enabled` (backward compatible), add
  `claude_compat_enabled`.
- UI labels: "模拟 Codex 客户端" / "模拟 Claude Code 客户端" (aligned with
  cc-switch naming).
