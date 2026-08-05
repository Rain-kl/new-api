# Sub2API Codex Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional Codex official-client compatibility layer to the existing Sub2API channel (type 59) so traffic to sub2api can pass `codex_cli_only` while preserving real CLI session keys or synthesizing sticky ones.

**Architecture:** Keep `ChannelTypeSub2API` and its URL/auth model. Add a pure helper package `relay/channel/codexcompat` for detection, sticky IDs, and header application. When `ChannelSettings.CodexCompatEnabled` is true, `sub2api.Adaptor` overrides header setup and Responses body conversion; otherwise behavior stays identical to today’s embedded `newapi.Adaptor`.

**Tech Stack:** Go 1.22+, Gin, existing relay adaptor pattern, `github.com/stretchr/testify`, React channel form + i18n.

**Spec:** `docs/superpowers/specs/2026-08-05-codex-gateway-channel-design.md`

## Global Constraints

- Do **not** add a new channel type; enhance type 59 only.
- Do **not** change ChatGPT Subscription Codex (type 57) behavior.
- Default `codex_compat_enabled=false` → zero behavior change for existing Sub2API channels.
- JSON via `common.Marshal` / `common.Unmarshal` only in business code.
- Backend tests: `require` for setup/fatal, `assert` for non-fatal checks.
- Frontend: `bun` in `web/`; user-facing strings via `t('English key')`.
- Never log API keys.

## File map

| Path | Role |
|------|------|
| `relay/channel/codexcompat/detect.go` | Official UA/originator + version parse + gate-capable check |
| `relay/channel/codexcompat/sticky.go` | Sticky ID resolution + UUID5 |
| `relay/channel/codexcompat/headers.go` | Apply preserve/synthesize headers to `http.Header` |
| `relay/channel/codexcompat/body.go` | prompt_cache_key inject; strip empty instructions |
| `relay/channel/codexcompat/*_test.go` | Unit tests |
| `relaykit/dto/channel_settings.go` | Settings fields + light validation |
| `relay/channel/sub2api/adaptor.go` | Wire compat into SetupRequestHeader / ConvertOpenAIResponsesRequest |
| `relay/channel/sub2api/adaptor_test.go` | Integration-style unit tests for adaptor |
| `web/src/features/channels/lib/channel-form.ts` | Zod + defaults + serialize |
| `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | UI fields for type 59 |
| `web/src/i18n/locales/*.json` | Translations (en keys + sync other locales as project practice) |

---

### Task 1: `codexcompat` detection helpers + tests

**Files:**
- Create: `relay/channel/codexcompat/detect.go`
- Create: `relay/channel/codexcompat/detect_test.go`

**Interfaces:**
- Produces:
  - `func ParseEngineVersion(userAgent string) (version string, ok bool)`
  - `func IsOfficialUserAgentStrict(userAgent string) bool`
  - `func IsOfficialOriginator(originator string) bool`
  - `func IsGateCapableOfficialCodex(userAgent, originator string) bool` — true only if (strict official UA **or** official originator) **and** `ParseEngineVersion(ua)` ok
  - Constants for default client name `codex_cli_rs` if useful

- [ ] **Step 1: Write failing tests** for gate-capable matrix

```go
package codexcompat_test

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/codexcompat"
	"github.com/stretchr/testify/assert"
)

func TestIsGateCapableOfficialCodex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, ua, originator string
		want                 bool
	}{
		{"cli ua", "codex_cli_rs/0.146.0 (linux; x86_64)", "codex_cli_rs", true},
		{"tui ua", "codex-tui/0.142.0 (Darwin; arm64)", "", true},
		{"originator only no version ua", "curl/8.0", "codex_cli_rs", false},
		{"go ua with originator", "Go-http-client/1.1", "codex_cli_rs", false},
		{"empty", "", "", false},
		{"evil substring ua", "Mozilla codex_cli_rs/0.1.0", "codex_cli_rs", false}, // strict prefix only
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, codexcompat.IsGateCapableOfficialCodex(tc.ua, tc.originator))
		})
	}
}

func TestParseEngineVersion(t *testing.T) {
	t.Parallel()
	v, ok := codexcompat.ParseEngineVersion("codex_cli_rs/0.146.0 (linux; x86_64)")
	assert.True(t, ok)
	assert.Equal(t, "0.146.0", v)
	_, ok = codexcompat.ParseEngineVersion("curl/8.0")
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd /Users/ryan/Code/Go/new-api && go test ./relay/channel/codexcompat/ -count=1
```

Expected: package or symbols not found.

- [ ] **Step 3: Implement `detect.go`**

Align with sub2api `request.go` official lists (strict HasPrefix for UA; exact originator set + `Codex ` family prefix). Keep lists in one place with a short comment referencing sub2api behavior.

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test ./relay/channel/codexcompat/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add relay/channel/codexcompat/detect.go relay/channel/codexcompat/detect_test.go
git commit -m "feat(codexcompat): add official client gate-capable detection"
```

---

### Task 2: Sticky IDs + header/body apply helpers

**Files:**
- Create: `relay/channel/codexcompat/sticky.go`
- Create: `relay/channel/codexcompat/headers.go`
- Create: `relay/channel/codexcompat/body.go`
- Create: `relay/channel/codexcompat/sticky_test.go`
- Create: `relay/channel/codexcompat/headers_test.go`
- Create: `relay/channel/codexcompat/body_test.go`

**Interfaces:**
- Produces:
  - `type IdentityMode string` with `auto`, `passthrough`, `synthesize`
  - `type StickyInput struct { ChannelID, UserID, TokenID int; SessionID, ThreadID, WindowID, PromptCacheKey, UserAgent, Originator string }`
  - `type StickyIDs struct { SessionID, ThreadID, WindowID, PromptCacheKey string }`
  - `func ResolveStickyIDs(in StickyInput) StickyIDs`
  - `type ApplyInput struct { Mode IdentityMode; ClientVersion, ClientName string; Sticky StickyIDs; Inbound http.Header; GateCapable bool }`
  - `func ApplyIdentityHeaders(dst *http.Header, in ApplyInput)` — mutates dst
  - `func PatchResponsesBodyJSON(body []byte, sticky StickyIDs, stripEmptyInstructions bool) ([]byte, error)`
  - `func HasInboundCodexFingerprint(h http.Header) bool` — any `x-codex-*` present
  - `func BuildSyntheticUserAgent(clientName, version string) string`

- [ ] **Step 1: Write failing tests**

Cover:

1. Same `ChannelID|UserID|TokenID` → identical sticky triple and cache key.
2. Inbound `prompt_cache_key` wins as thread + cache key.
3. Inbound thread header wins over token seed.
4. `PatchResponsesBodyJSON` injects `prompt_cache_key` when missing; strips `"instructions":""` when flag true; leaves non-empty instructions.
5. `ApplyIdentityHeaders` synthesize sets UA with version, originator, session, thread, window.
6. Preserve path keeps client session when gate-capable; injects window only if no `x-codex-*`.

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./relay/channel/codexcompat/ -count=1
```

- [ ] **Step 3: Implement sticky/headers/body**

Use `github.com/google/uuid` if already in go.mod; otherwise SHA256 truncate to UUID layout without new deps if uuid not present. Check:

```bash
grep google/uuid go.mod || true
```

Prefer existing project UUID helper if any (`common` package). Sticky UUID5: use `uuid.NewSHA1(namespace, []byte(seed))` from `google/uuid` **only if already depended**; else deterministic hex formatting from sha256 is fine as long as stable and ≤36–64 chars.

Header apply algorithm (auto):

```text
if mode==synthesize OR (mode==auto && !gateCapable):
  set synthetic UA, originator
  set session/thread/window from StickyIDs (after ResolveStickyIDs merged inbound)
else: // preserve
  copy inbound UA, originator, session, thread, all x-codex-* if present
  if !HasInboundCodexFingerprint: set x-codex-window-id from sticky
```

Always set sticky session/thread on synthesize path even if inbound empty.

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test ./relay/channel/codexcompat/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add relay/channel/codexcompat/
git commit -m "feat(codexcompat): sticky ids, header apply, body prompt_cache_key"
```

---

### Task 3: ChannelSettings fields + validation

**Files:**
- Modify: `relaykit/dto/channel_settings.go`
- Create or modify tests under `relaykit/dto/` if settings validation tests exist; else `relaykit/dto/channel_settings_codex_compat_test.go`

**Interfaces:**
- Produces fields on `ChannelSettings`:

```go
CodexCompatEnabled   bool   `json:"codex_compat_enabled,omitempty"`
CodexClientVersion   string `json:"codex_client_version,omitempty"`
CodexClientName      string `json:"codex_client_name,omitempty"`
CodexIdentityMode    string `json:"codex_identity_mode,omitempty"` // auto|passthrough|synthesize
```

- Produces: `func (s *ChannelSettings) ValidateCodexCompat() error`

Rules:

- If `!CodexCompatEnabled` → nil.
- Mode empty → treat as `auto`.
- Mode must be auto|passthrough|synthesize.
- If enabled and mode is auto or synthesize: `CodexClientVersion` must match `^\d+\.\d+\.\d+` (allow optional suffix after triple if desired; minimum triple required).
- Default name when empty at runtime: `codex_cli_rs` (validation need not require name).

Wire validation into existing channel save path if there is a central `ChannelSettings.Validate*` aggregator; search:

```bash
rg -n "ValidateHTTPTransport|ValidateMessagesRoleCompatibility" --glob '*.go' | head
```

Call `ValidateCodexCompat` from the same places those validators run.

- [ ] **Step 1: Write validation tests**
- [ ] **Step 2: Implement fields + ValidateCodexCompat + wire-up**
- [ ] **Step 3: `go test` affected packages**
- [ ] **Step 4: Commit**

```bash
git commit -m "feat(settings): add Sub2API codex_compat channel settings"
```

---

### Task 4: Wire Sub2API adaptor

**Files:**
- Modify: `relay/channel/sub2api/adaptor.go`
- Modify: `relay/channel/sub2api/adaptor_test.go`

**Interfaces:**
- Consumes: `codexcompat.*`, `info.ChannelSetting`, `info` user/token/channel ids from `RelayInfo` / gin context keys used elsewhere for user id and token id.
- Overrides:
  - `SetupRequestHeader`
  - `ConvertOpenAIResponsesRequest` (and ensure compact mode still works via same conversion path)

Discover how to read user id / token id from `RelayInfo` or context:

```bash
rg -n "UserId|TokenId|token_id|UserId" relay/common/relay_info.go | head -40
```

Use those fields for sticky seed; if token id missing, fall back to `user_id` only + channel id.

**SetupRequestHeader algorithm:**

```go
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.Adaptor.SetupRequestHeader(c, req, info); err != nil {
		return err
	}
	if info == nil || !info.ChannelSetting.CodexCompatEnabled {
		return nil
	}
	// compact Accept
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		req.Set("Accept", "application/json")
	}
	req.Set("Content-Type", "application/json")

	ua := c.Request.Header.Get("User-Agent")
	originator := c.Request.Header.Get("originator")
	gate := codexcompat.IsGateCapableOfficialCodex(ua, originator)
	mode := codexcompat.NormalizeMode(info.ChannelSetting.CodexIdentityMode)

	stickyIn := codexcompat.StickyInput{ /* fill from c headers + info + gjson prompt_cache_key if body available */ }
	// If body not available at header time, resolve sticky without prompt_cache_key;
	// ConvertOpenAIResponsesRequest will align body key to thread id.

	ids := codexcompat.ResolveStickyIDs(stickyIn)
	codexcompat.ApplyIdentityHeaders(req, codexcompat.ApplyInput{
		Mode: mode, ClientVersion: info.ChannelSetting.CodexClientVersion,
		ClientName: firstNonEmpty(info.ChannelSetting.CodexClientName, "codex_cli_rs"),
		Sticky: ids, Inbound: c.Request.Header, GateCapable: gate,
	})
	// store sticky on info or context for body step if needed
	return nil
}
```

Prefer storing resolved sticky on `RelayInfo` if a field exists or use `common.SetContextKey` — avoid package-level maps.

If `RelayInfo` has no good field, add:

```go
// on RelayInfo or a small context key in constant package
CodexCompatSticky *codexcompat.StickyIDs
```

only if necessary; keep minimal.

**ConvertOpenAIResponsesRequest:**

```go
func (a *Adaptor) ConvertOpenAIResponsesRequest(...) (any, error) {
	out, err := a.Adaptor.ConvertOpenAIResponsesRequest(c, info, request)
	// if compat off return
	// else marshal request, PatchResponsesBodyJSON with stripEmptyInstructions=true, unmarshal back
	// OR mutate request.PromptCacheKey and Instructions fields directly on dto
}
```

Prefer mutating DTO fields over full re-marshal when types allow (`PromptCacheKey` is often `json.RawMessage` — check `dto.OpenAIResponsesRequest`).

For compact: same conversion; `SetupRequestHeader` already forced Accept.

- [ ] **Step 1: Write adaptor tests** with `httptest` + gin test context:

1. Compat off → only `Authorization` from parent (no forced originator).
2. Compat on synthesize → UA contains version, originator set, x-codex-window-id set.
3. Compat on + gate-capable inbound UA → preserves session_id.
4. Compact mode → Accept application/json.
5. Convert injects prompt_cache_key when empty.

- [ ] **Step 2: Implement adaptor overrides**
- [ ] **Step 3: `go test ./relay/channel/sub2api/ ./relay/channel/codexcompat/ -count=1`**
- [ ] **Step 4: Commit**

```bash
git commit -m "feat(sub2api): wire optional Codex client compatibility layer"
```

---

### Task 5: Frontend settings for Sub2API

**Files:**
- Modify: `web/src/features/channels/lib/channel-form.ts`
- Modify: `web/src/features/channels/lib/channel-form-errors.ts` if field list needed
- Modify: `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- Modify: `web/src/features/channels/types.ts` if settings type listed
- Modify: `web/src/i18n/locales/en.json` (and run i18n sync or fill zh at minimum per project practice)

**UI (show when channel type === 59):**

- Switch: `Codex compatibility`
- When on:
  - Input: `Codex client version` (placeholder `0.146.0`)
  - Input optional: `Codex client name` default empty → backend `codex_cli_rs`
  - Select: identity mode Auto / Passthrough / Synthesize
- FormDescription explaining CLI preserve vs synthetic sticky; sub2api `codex_cli_only`

Serialize into channel settings JSON fields matching backend tags.

- [ ] **Step 1: Add zod fields + parse/serialize in channel-form.ts**
- [ ] **Step 2: Add form controls in drawer near other Sub2API / advanced toggles**
- [ ] **Step 3: Add i18n English keys; sync locales**

```bash
cd web && bun run i18n:sync
```

(or project’s documented i18n command)

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(web): Sub2API Codex compatibility channel settings UI"
```

---

### Task 6: End-to-end verification checklist (local)

**Files:** none required beyond prior

- [ ] **Step 1: Unit suite**

```bash
cd /Users/ryan/Code/Go/new-api
go test ./relay/channel/codexcompat/ ./relay/channel/sub2api/ ./relaykit/dto/ -count=1
```

Expected: all PASS.

- [ ] **Step 2: Manual optional (if sub2api available)**

1. Create/edit Sub2API channel, enable compat, set version `0.146.0`.
2. `curl` Responses without CLI headers → expect success when upstream account has `codex_cli_only` (or at least request leaves new-api with full identity headers — capture via debug proxy).
3. Disable compat → outbound lacks synthetic originator/x-codex.

- [ ] **Step 3: Final commit only if fixes needed; otherwise done**

---

## Spec coverage self-check

| Spec requirement | Task |
|------------------|------|
| Enhance type 59 only | 3–4 |
| Default off | 3–4 |
| Gate-capable detection | 1 |
| Sticky IDs + prompt_cache_key | 2, 4 |
| Preserve vs synthesize auto | 2, 4 |
| Compact Accept JSON | 4 |
| Strip empty instructions | 2, 4 |
| Settings + validation | 3 |
| Frontend | 5 |
| Channel 57 unchanged | no edits to codex/ |
| Tests | 1, 2, 4, 6 |

## Placeholder scan

No TBD steps; concrete commands and interfaces included.

## Type consistency

- Settings JSON tags: `codex_compat_enabled`, `codex_client_version`, `codex_client_name`, `codex_identity_mode`
- Package name: `codexcompat`
- Mode strings: `auto` | `passthrough` | `synthesize`
