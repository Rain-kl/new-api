# Advanced Custom Client Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add "Simulate Codex client" and "Simulate Claude Code client" toggles to the Advanced Custom channel (58), remove the old Sub2API (59) codex-compat layer and the per-channel `chat_completions_to_responses` toggle, making Advanced Custom the single home for client emulation.

**Architecture:** Advanced Custom routes already convert protocols via `relaykit/relayconvert`. New channel settings (`codex_compat_enabled` already exists; `claude_compat_enabled` is new) gate header shaping in `relay/channel/advancedcustom/adaptor.go` `SetupRequestHeader`, plus a system-prompt identity prepend on the converted `*dto.ClaudeRequest` for Claude-targeted routes. Sub2API reverts to plain `newapi.Adaptor` passthrough; the `relay/channel/codexcompat/` package is deleted. The `chat_completions_to_responses` channel force-toggle and its service refactor are reverted.

**Tech Stack:** Go 1.22+ (Gin, GORM), React 19 + TypeScript + Bun, i18next flat JSON locales.

## Global Constraints

- **relaykit independence:** any change under `relaykit/` must verify `cd relaykit && GOWORK=off go build ./...`; a root-module build is not sufficient.
- **JSON wrapper:** all marshal/unmarshal in new Go code must use `common.Marshal`/`common.Unmarshal` (or `kitutil.*` inside `relaykit/relayconvert`), never direct `encoding/json` calls. Type references like `json.RawMessage` are fine.
- **Tests:** new/rewritten Go tests use `github.com/stretchr/testify/require` for setup/fatal and `assert` for value checks. Deterministic table tests with exact expected values.
- **No sticky ID synthesis this round:** do not port deterministic session/thread/window/prompt_cache_key synthesis. Advanced Custom relies on its own converters. `applyCodexIdentityHeaders` only sets UA/originator and passes through client-provided `session_id`/`thread_id`/`x-codex-*`.
- **Naming:** UI labels are "Simulate Codex client" (模拟 Codex 客户端) and "Simulate Claude Code client" (模拟 Claude Code 客户端). JSON fields stay `codex_compat_enabled` / `claude_compat_enabled`.
- **i18n:** every user-facing string must exist in all 7 locales (`en`, `zh`, `zh-TW`, `fr`, `ru`, `ja`, `vi`). Use `bun run i18n:sync` from `web/`. Follow the project's `i18n-translate` skill for locale edits.
- **Protected branding:** do not modify any new-api / QuantumNous branding, module paths, package names, or attribution.
- **Frontend tooling:** use `bun` (not npm/pnpm/yarn) in `web/`.
- **Execution note (this plan):** Tasks 1-3 are backend and sequential (they share `relaykit/dto/channel_settings.go`). Task 4 (frontend) touches only `web/src/...` and may run in parallel with backend tasks. To avoid git index races between parallel implementers, implementers must NOT run `git add`/`git commit`; the controller commits each task's working-tree changes after the implementer reports, then dispatches the task reviewer against that commit.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `relaykit/dto/channel_settings.go` | Channel settings fields + `ValidateCodexCompat` |
| `relay/claude_handler.go`, `relay/compatible_handler.go` | Revert cc→responses force toggle |
| `service/openai_chat_responses_mode.go` | Remove `ShouldChatCompletionsUseResponses` (force variant) |
| `relay/channel/sub2api/adaptor.go` | Revert to plain passthrough |
| `relay/channel/codexcompat/` (delete) | Old sticky codex layer (gone) |
| `relay/channel/advancedcustom/compat.go` (new) | `applyCodexIdentityHeaders`, `applyClaudeCodeHeaders`, `prependClaudeCodeSystemPrompt` |
| `relay/channel/advancedcustom/adaptor.go` | Wire compat hooks into header setup + convert paths |
| `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | Advanced Custom compat UI; remove Sub2API/cc→responses UI |
| `web/src/features/channels/lib/channel-form.ts` | Form schema/defaults/serialize |
| `web/src/features/channels/lib/channel-form-errors.ts` | Error key list |
| `web/src/features/channels/types.ts` | Channel settings TS type |
| `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json` | i18n keys |

---

### Task 1: Backend — Remove per-channel `chat_completions_to_responses` toggle

**Files:**
- Modify: `relaykit/dto/channel_settings.go` (remove `ChatCompletionsToResponses` field, ~lines 50-56)
- Modify: `relay/claude_handler.go:168`
- Modify: `relay/compatible_handler.go:88`
- Modify: `service/openai_chat_responses_mode.go`
- Test: `service/openai_chat_responses_mode_test.go`

**Interfaces:**
- Consumes: existing `service.ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool` (stays).
- Produces: `service.ShouldChatCompletionsUseResponses(...)` deleted; `dto.ChannelSettings.ChatCompletionsToResponses` deleted.

- [ ] **Step 1: Remove the DTO field**

In `relaykit/dto/channel_settings.go` delete the whole `ChatCompletionsToResponses` block:

```go
	// ChatCompletionsToResponses converts inbound Chat Completions requests to
	// the OpenAI Responses API before calling upstream. Intended for Sub2API
	// (and similar) gateways that prefer /v1/responses. Default false.
	// When true, all chat-completions traffic on this channel is converted
	// regardless of the global chat_completions_to_responses_policy.
	ChatCompletionsToResponses bool `json:"chat_completions_to_responses,omitempty"`
```

- [ ] **Step 2: Revert handler call sites**

`relay/claude_handler.go` — replace:

```go
		service.ShouldChatCompletionsUseResponses(
			info.ChannelId,
			info.ChannelType,
			info.OriginModelName,
			info.ChannelSetting.ChatCompletionsToResponses,
		) {
```

with:

```go
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
```

`relay/compatible_handler.go` — make the identical replacement at its call site (around line 88).

- [ ] **Step 3: Remove the service force variant**

In `service/openai_chat_responses_mode.go` delete this function:

```go
// ShouldChatCompletionsUseResponses reports whether chat completions should be
// upgraded to the Responses API for this request.
//
// Order:
//  1. Channel types that registered a skip (Echo, etc.) never convert.
//  2. Channel setting chat_completions_to_responses forces conversion for all models.
//  3. Otherwise fall back to the global policy (channel allowlist + model patterns).
func ShouldChatCompletionsUseResponses(channelID int, channelType int, model string, channelForce bool) bool {
	if common.ShouldSkipChatCompletionsToResponses(channelType) {
		return false
	}
	if channelForce {
		return true
	}
	return ShouldChatCompletionsUseResponsesGlobal(channelID, channelType, model)
}
```

Keep `ShouldChatCompletionsUseResponsesPolicy` and `ShouldChatCompletionsUseResponsesGlobal` unchanged. `common` import stays (still used by `ShouldChatCompletionsUseResponsesPolicy`).

- [ ] **Step 4: Rewrite the tests**

Replace the whole body of `service/openai_chat_responses_mode_test.go` with:

```go
package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldChatCompletionsUseResponsesGlobal_SkipChannelType(t *testing.T) {
	const skipType = 999001
	common.RegisterSkipChatCompletionsToResponses(skipType)
	require.True(t, common.ShouldSkipChatCompletionsToResponses(skipType))
	assert.False(t, ShouldChatCompletionsUseResponsesGlobal(1, skipType, "any-model"))
}

func TestShouldChatCompletionsUseResponsesGlobal_GlobalPolicy(t *testing.T) {
	orig := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-5"},
	}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = orig
	})

	assert.True(t, ShouldChatCompletionsUseResponsesGlobal(1, constant.ChannelTypeOpenAI, "gpt-5.1"))
	assert.False(t, ShouldChatCompletionsUseResponsesGlobal(1, constant.ChannelTypeOpenAI, "claude-4"))
}
```

- [ ] **Step 5: Run tests and build**

Run:
```bash
go build ./...
go test ./service/ -run TestShouldChatCompletionsUseResponsesGlobal -v
cd relaykit && GOWORK=off go build ./...
```
Expected: build passes; the two global tests pass; no other package references `ChatCompletionsToResponses` or `ShouldChatCompletionsUseResponses(` (grep to confirm: `grep -rn "ChatCompletionsToResponses\|ShouldChatCompletionsUseResponses(" --include="*.go"` should only show the global/policy functions and `chat_completions_to_responses_policy` global config).

- [ ] **Step 6: Commit (controller)**

```bash
git add relaykit/dto/channel_settings.go relay/claude_handler.go relay/compatible_handler.go service/openai_chat_responses_mode.go service/openai_chat_responses_mode_test.go
git commit -m "refactor(relay): drop per-channel chat_completions_to_responses toggle"
```

---

### Task 2: Backend — Remove Sub2API codex compat and the codexcompat package

**Files:**
- Modify: `relaykit/dto/channel_settings.go` (remove `CodexIdentityMode` field + constants; simplify `ValidateCodexCompat`)
- Modify: `relay/channel/sub2api/adaptor.go` (revert to plain passthrough)
- Delete: `relay/channel/codexcompat/` (all 8 source + 5 test files)
- Test: `relaykit/dto/channel_settings_codex_compat_test.go` (rewrite)

**Interfaces:**
- Consumes: nothing new; deletes `dto.CodexIdentityMode*` constants and `codexcompat` package.
- Produces: `dto.ChannelSettings.CodexCompatEnabled/CodexClientVersion/CodexClientName` remain; `ValidateCodexCompat` now only requires a valid version when enabled. `dto.DefaultCodexClientName` stays for Task 3.

- [ ] **Step 1: Remove the identity-mode field and constants**

In `relaykit/dto/channel_settings.go`:

Delete the field block:

```go
	// CodexIdentityMode controls identity header policy: auto|passthrough|synthesize.
	// Empty is treated as auto.
	CodexIdentityMode string `json:"codex_identity_mode,omitempty"`
```

Delete the constants:

```go
	CodexIdentityModeAuto        = "auto"
	CodexIdentityModePassthrough = "passthrough"
	CodexIdentityModeSynthesize  = "synthesize"
```

Keep `DefaultCodexClientName = "codex_cli_rs"` and `codexClientVersionPattern`.

- [ ] **Step 2: Simplify `ValidateCodexCompat`**

Replace the whole function with:

```go
// ValidateCodexCompat validates "Simulate Codex client" channel settings.
// No-op when CodexCompatEnabled is false.
func (s *ChannelSettings) ValidateCodexCompat() error {
	if s == nil || !s.CodexCompatEnabled {
		return nil
	}
	version := strings.TrimSpace(s.CodexClientVersion)
	if version == "" {
		return fmt.Errorf("codex_client_version is required when codex_compat is enabled")
	}
	if !codexClientVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid codex_client_version: %s", s.CodexClientVersion)
	}
	return nil
}
```

`strings` is still used elsewhere in the file — do not remove the import.

- [ ] **Step 3: Revert the Sub2API adaptor**

Replace the entire contents of `relay/channel/sub2api/adaptor.go` with:

```go
package sub2api

import (
	"github.com/QuantumNous/new-api/relay/channel/newapi"
)

type Adaptor struct {
	newapi.Adaptor
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
```

- [ ] **Step 4: Delete the codexcompat package**

```bash
git rm -r relay/channel/codexcompat
```

- [ ] **Step 5: Rewrite the DTO test**

Replace the entire contents of `relaykit/dto/channel_settings_codex_compat_test.go` with:

```go
package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSettingsValidateCodexCompat(t *testing.T) {
	t.Parallel()

	t.Run("disabled skips validation", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, (&ChannelSettings{}).ValidateCodexCompat())
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: false,
			CodexClientVersion: "not-a-version",
		}).ValidateCodexCompat())
		require.NoError(t, (*ChannelSettings)(nil).ValidateCodexCompat())
	})

	t.Run("enabled requires version", func(t *testing.T) {
		t.Parallel()
		err := (&ChannelSettings{CodexCompatEnabled: true}).ValidateCodexCompat()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "codex_client_version")
	})

	t.Run("valid versions pass", func(t *testing.T) {
		t.Parallel()
		for _, version := range []string{"0.146.0", "1.2.3-beta", "10.0.0"} {
			require.NoError(t, (&ChannelSettings{
				CodexCompatEnabled: true,
				CodexClientVersion: version,
			}).ValidateCodexCompat(), "version=%q", version)
		}
	})

	t.Run("invalid versions fail", func(t *testing.T) {
		t.Parallel()
		for _, version := range []string{"v1.2.3", "1.2", "abc", " 0.146.0 "} {
			err := (&ChannelSettings{
				CodexCompatEnabled: true,
				CodexClientVersion: version,
			}).ValidateCodexCompat()
			require.Error(t, err, "version=%q", version)
			assert.Contains(t, err.Error(), "codex_client_version")
		}
	})

	t.Run("name is optional at validation time", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexClientVersion: "0.1.0",
		}).ValidateCodexCompat())
		assert.Equal(t, "codex_cli_rs", DefaultCodexClientName)
	})
}

func TestChannelSettingsCodexCompatJSONRoundTrip(t *testing.T) {
	t.Parallel()

	empty, err := json.Marshal(ChannelSettings{})
	require.NoError(t, err)
	assert.NotContains(t, string(empty), "codex_compat")
	assert.NotContains(t, string(empty), "codex_client")
	assert.NotContains(t, string(empty), "codex_identity")
	assert.NotContains(t, string(empty), "chat_completions_to_responses")

	src := ChannelSettings{
		CodexCompatEnabled: true,
		CodexClientVersion: "0.146.0",
		CodexClientName:    "codex_cli_rs",
	}
	encoded, err := json.Marshal(src)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"codex_compat_enabled":true`)
	assert.Contains(t, string(encoded), `"codex_client_version":"0.146.0"`)
	assert.Contains(t, string(encoded), `"codex_client_name":"codex_cli_rs"`)
	assert.NotContains(t, string(encoded), "codex_identity")
	assert.NotContains(t, string(encoded), "chat_completions_to_responses")

	var decoded ChannelSettings
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, src, decoded)
}
```

- [ ] **Step 6: Run tests and build**

Run:
```bash
go build ./...
go test ./relaykit/dto/ -run 'TestChannelSettingsValidateCodexCompat|TestChannelSettingsCodexCompatJSONRoundTrip' -v
go test ./relay/channel/sub2api/ -v
cd relaykit && GOWORK=off go build ./...
```
Expected: build passes; DTO tests pass; sub2api tests (plain passthrough: `TestGetRequestURLAlphaSearch`, `TestAdaptorInheritsNewAPIResponsesCompactSupport`) pass after their codex-specific tests are removed by this task. **The sub2api test file still contains codex tests — delete those test functions too** (`TestSetupRequestHeader_*`, `TestConvertOpenAIResponsesRequest_*`) and the now-unused imports (`encoding/json`, `codexcompat`, `dto` usage, `testGinContext`/`baseRelayInfo` helpers if unused).

- [ ] **Step 7: Commit (controller)**

```bash
git add -A relaykit/dto/channel_settings.go relaykit/dto/channel_settings_codex_compat_test.go relay/channel/sub2api
git rm -r relay/channel/codexcompat
git commit -m "refactor(channel): remove Sub2API codex compat; delete codexcompat package"
```

---

### Task 3: Backend — Advanced Custom "Simulate Codex client" + "Simulate Claude Code client"

**Files:**
- Modify: `relaykit/dto/channel_settings.go` (add `ClaudeCompatEnabled`)
- Create: `relay/channel/advancedcustom/compat.go`
- Modify: `relay/channel/advancedcustom/adaptor.go`
- Test: `relay/channel/advancedcustom/compat_test.go` (new)

**Interfaces:**
- Consumes: `dto.ChannelSettings.CodexCompatEnabled/CodexClientVersion/CodexClientName/ClaudeCompatEnabled`; `dto.DefaultCodexClientName`; existing `relay/channel/advancedcustom` helpers `shouldApplyClaudeHeaders`, `applyClaudeHeaders`, `resolve`/`a.converter`.
- Produces (package `advancedcustom`, same package so unexported):
  - `func applyCodexIdentityHeaders(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo)`
  - `func applyClaudeCodeHeaders(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo)`
  - `func prependClaudeCodeSystemPrompt(request *dto.ClaudeRequest)`
  - consts `claudeCodeUserAgent`, `claudeCodeBeta`, `claudeCodeSystemIdentity`, `defaultCodexClientVersion`

- [ ] **Step 1: Add the Claude compat setting**

In `relaykit/dto/channel_settings.go`, after the codex fields add:

```go
	// ClaudeCompatEnabled enables Advanced Custom "Simulate Claude Code client"
	// fingerprint headers/body on routes that target Claude Messages. Default
	// false (no behavior change).
	ClaudeCompatEnabled bool `json:"claude_compat_enabled,omitempty"`
```

- [ ] **Step 2: Write the failing tests**

Create `relay/channel/advancedcustom/compat_test.go`:

```go
package advancedcustom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codexCompatRelayInfo(enabled bool, version string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat:     "openai-responses",
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "gpt-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "sk-test",
			ChannelBaseUrl:    "https://upstream.example",
			ChannelType:       constant.ChannelTypeAdvancedCustom,
			UpstreamModelName: "gpt-5",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: []dto.AdvancedCustomRoute{
						{
							IncomingPath: "/v1/responses",
							UpstreamPath: "/v1/responses",
							Converter:    "none",
						},
					},
				},
			},
			ChannelSetting: dto.ChannelSettings{
				CodexCompatEnabled: enabled,
				CodexClientVersion: version,
			},
		},
	}
}

func compatGinContext(headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

func TestApplyCodexIdentityHeaders(t *testing.T) {
	t.Run("enabled sets UA and originator with defaults", func(t *testing.T) {
		header := http.Header{}
		applyCodexIdentityHeaders(compatGinContext(nil), &header, codexCompatRelayInfo(true, "0.146.0"))
		assert.True(t, strings.HasPrefix(header.Get("User-Agent"), "codex_cli_rs/0.146.0"))
		assert.Equal(t, "codex_cli_rs", header.Get("originator"))
	})

	t.Run("enabled passthrough client x-codex headers", func(t *testing.T) {
		header := http.Header{}
		c := compatGinContext(map[string]string{
			"session_id":       "client-session",
			"thread_id":        "client-thread",
			"X-Codex-Window-Id": "client-window",
		})
		applyCodexIdentityHeaders(c, &header, codexCompatRelayInfo(true, "0.146.0"))
		assert.Equal(t, "client-session", header.Get("session_id"))
		assert.Equal(t, "client-thread", header.Get("thread_id"))
		assert.Equal(t, "client-window", header.Get("x-codex-window-id"))
	})

	t.Run("enabled does not synthesize IDs when absent", func(t *testing.T) {
		header := http.Header{}
		applyCodexIdentityHeaders(compatGinContext(nil), &header, codexCompatRelayInfo(true, "0.146.0"))
		assert.Empty(t, header.Get("session_id"))
		assert.Empty(t, header.Get("thread_id"))
		assert.Empty(t, header.Get("x-codex-window-id"))
	})

	t.Run("custom client name and default version", func(t *testing.T) {
		header := http.Header{}
		info := codexCompatRelayInfo(true, "")
		info.ChannelSetting.CodexClientName = "codex-tui"
		applyCodexIdentityHeaders(compatGinContext(nil), &header, info)
		assert.True(t, strings.HasPrefix(header.Get("User-Agent"), "codex-tui/"+defaultCodexClientVersion))
		assert.Equal(t, "codex-tui", header.Get("originator"))
	})
}

func TestApplyClaudeCodeHeaders(t *testing.T) {
	t.Run("sets claude fingerprint headers", func(t *testing.T) {
		header := http.Header{}
		applyClaudeCodeHeaders(compatGinContext(nil), &header, codexCompatRelayInfo(false, ""))
		assert.Equal(t, claudeCodeUserAgent, header.Get("User-Agent"))
		assert.Equal(t, "cli", header.Get("x-app"))
		assert.Equal(t, claudeCodeBeta, header.Get("anthropic-beta"))
	})

	t.Run("preserves existing anthropic-beta and prepends marker", func(t *testing.T) {
		header := http.Header{}
		c := compatGinContext(map[string]string{"anthropic-beta": "some-beta-1"})
		applyClaudeCodeHeaders(c, &header, codexCompatRelayInfo(false, ""))
		assert.Equal(t, claudeCodeBeta+",some-beta-1", header.Get("anthropic-beta"))
	})

	t.Run("does not duplicate marker when already present", func(t *testing.T) {
		header := http.Header{}
		c := compatGinContext(map[string]string{"anthropic-beta": claudeCodeBeta + ",other"})
		applyClaudeCodeHeaders(c, &header, codexCompatRelayInfo(false, ""))
		assert.Equal(t, claudeCodeBeta+",other", header.Get("anthropic-beta"))
	})
}

func TestPrependClaudeCodeSystemPrompt(t *testing.T) {
	t.Run("string system becomes identity + original", func(t *testing.T) {
		req := &dto.ClaudeRequest{System: "Be helpful."}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 2)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
		assert.Equal(t, "Be helpful.", *blocks[1].Text)
	})

	t.Run("array system gets identity first", func(t *testing.T) {
		orig := dto.ClaudeMediaMessage{Type: "text"}
		orig.SetText("Existing system.")
		req := &dto.ClaudeRequest{System: []dto.ClaudeMediaMessage{orig}}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 2)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
		assert.Equal(t, "Existing system.", *blocks[1].Text)
	})

	t.Run("idempotent when identity already first", func(t *testing.T) {
		identity := dto.ClaudeMediaMessage{Type: "text"}
		identity.SetText(claudeCodeSystemIdentity)
		req := &dto.ClaudeRequest{System: []dto.ClaudeMediaMessage{identity}}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 1)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
	})

	t.Run("nil system gets identity", func(t *testing.T) {
		req := &dto.ClaudeRequest{}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 1)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
	})
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./relay/channel/advancedcustom/ -run 'TestApplyCodexIdentityHeaders|TestApplyClaudeCodeHeaders|TestPrependClaudeCodeSystemPrompt' -v`
Expected: FAIL — undefined functions/consts.

- [ ] **Step 4: Implement `compat.go`**

Create `relay/channel/advancedcustom/compat.go`:

```go
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
// (session_id, thread_id, x-codex-*) unchanged. It never synthesizes IDs.
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
		if first := system[0].GetText(); first != nil && *first == claudeCodeSystemIdentity {
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
```

Note: import path is `relaycommon "github.com/QuantumNous/new-api/relay/common"` — keep the alias consistent with `adaptor.go` (`relaycommon`).

- [ ] **Step 5: Wire into `adaptor.go`**

In `relay/channel/advancedcustom/adaptor.go`:

**5a.** `SetupRequestHeader` — after the existing `if shouldApplyClaudeHeaders(a.converter, info) { applyClaudeHeaders(c, header, info) }` block, add:

```go
	if info.ChannelSetting.CodexCompatEnabled {
		applyCodexIdentityHeaders(c, header, info)
	}
	if info.ChannelSetting.ClaudeCompatEnabled && shouldApplyClaudeHeaders(a.converter, info) {
		applyClaudeCodeHeaders(c, header, info)
	}
```

**5b.** `ConvertOpenAIRequest` case `relayconvert.ConverterOpenAIChatToClaudeMessages` — inject after conversion succeeds, before `return result.Value, nil`:

```go
		if info.ChannelSetting.ClaudeCompatEnabled {
			if claudeReq, ok := result.Value.(*dto.ClaudeRequest); ok {
				prependClaudeCodeSystemPrompt(claudeReq)
			}
		}
```

**5c.** `ConvertOpenAIResponsesRequest` case `relayconvert.ConverterOpenAIResponsesToClaudeMessages` — after `claudeRequest, ok := result.Value.(*dto.ClaudeRequest)` succeeds, before `return claudeRequest, nil`:

```go
		if info.ChannelSetting.ClaudeCompatEnabled {
			prependClaudeCodeSystemPrompt(claudeRequest)
		}
```

**5d.** `ConvertClaudeRequest` case `relayconvert.ConverterNone` — replace:

```go
	case relayconvert.ConverterNone:
		return a.claudeAdaptor.ConvertClaudeRequest(c, info, request)
```

with:

```go
	case relayconvert.ConverterNone:
		claudeRequest, err := a.claudeAdaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			return nil, err
		}
		if info.ChannelSetting.ClaudeCompatEnabled {
			if claudeReq, ok := claudeRequest.(*dto.ClaudeRequest); ok {
				prependClaudeCodeSystemPrompt(claudeReq)
			}
		}
		return claudeRequest, nil
```

- [ ] **Step 6: Run tests and build**

Run:
```bash
go build ./...
go test ./relay/channel/advancedcustom/ -v
go test ./relaykit/dto/ -run 'TestChannelSettings' -v
cd relaykit && GOWORK=off go build ./...
```
Expected: all pass. Also verify `grep -rn "CodexIdentityMode" --include="*.go"` returns nothing.

- [ ] **Step 7: Commit (controller)**

```bash
git add relaykit/dto/channel_settings.go relay/channel/advancedcustom/compat.go relay/channel/advancedcustom/compat_test.go relay/channel/advancedcustom/adaptor.go
git commit -m "feat(channel): add simulate codex/claude-code client compat to advanced custom"
```

---

### Task 4: Frontend — Advanced Custom client compat UI, form, types, i18n

**Files:**
- Modify: `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- Modify: `web/src/features/channels/lib/channel-form.ts`
- Modify: `web/src/features/channels/lib/channel-form-errors.ts`
- Modify: `web/src/features/channels/types.ts`
- Modify: `web/src/i18n/locales/en.json`, `zh.json`, `zh-TW.json`, `fr.json`, `ru.json`, `ja.json`, `vi.json`

**Interfaces:**
- Consumes: backend JSON field names `codex_compat_enabled`, `codex_client_version`, `codex_client_name`, `claude_compat_enabled`. Existing frontend consts `DEFAULT_CODEX_CLIENT_VERSION = '0.146.0'` (`channel-form.ts:65`), `CHANNEL_TYPE_ADVANCED_CUSTOM`, `CHANNEL_TYPE_SUB2API`.
- Produces: form schema/type fields `codex_compat_enabled?`, `codex_client_version?`, `codex_client_name?`, `claude_compat_enabled?`; no `codex_identity_mode` / `chat_completions_to_responses`.

- [ ] **Step 1: Update the channel settings TS type**

In `web/src/features/channels/types.ts` replace lines ~95-99:

```ts
  codex_compat_enabled?: boolean
  codex_client_version?: string
  codex_client_name?: string
  codex_identity_mode?: 'auto' | 'passthrough' | 'synthesize' | string
  chat_completions_to_responses?: boolean
```

with:

```ts
  codex_compat_enabled?: boolean
  codex_client_version?: string
  codex_client_name?: string
  claude_compat_enabled?: boolean
```

- [ ] **Step 2: Update the form schema**

In `web/src/features/channels/lib/channel-form.ts`:

- Remove the `codex_identity_mode` schema entry (currently `codex_identity_mode: z.string().optional()` around line 294) and the `chat_completions_to_responses` entry (line 302).
- Add after `codex_client_name`:

```ts
    claude_compat_enabled: z.boolean().optional(),
```

- Update the zod refine that validates codex compat (around lines 413-431): it currently reads `data.codex_identity_mode`; simplify to only require `codex_client_version` (matching `X.Y.Z`) when `codex_compat_enabled === true`. Keep using `DEFAULT_CODEX_CLIENT_VERSION` as the placeholder/default.
- In the defaults object (around line 587-591) remove `codex_identity_mode` and `chat_completions_to_responses`, keep `codex_compat_enabled: false`, `codex_client_version: DEFAULT_CODEX_CLIENT_VERSION`, `codex_client_name: ''`, and add `claude_compat_enabled: false`.
- In the empty-edit defaults (around line 638-645) do the same.
- In the parse (around line 667-713): drop `codex_identity_mode` and `chat_completions_to_responses` parsing; add `claude_compat_enabled: parsed.claude_compat_enabled === true`.
- In serialization (around line 882-890): keep `codex_compat_enabled`/`codex_client_version`/`codex_client_name`; drop `codex_identity_mode` and `chat_completions_to_responses`; add:

```ts
    if (formData.claude_compat_enabled === true) {
      settingObj.claude_compat_enabled = true
    }
```

- [ ] **Step 3: Update error key list**

In `web/src/features/channels/lib/channel-form-errors.ts` remove `'codex_identity_mode'` and `'chat_completions_to_responses'` from the list at lines ~50-54.

- [ ] **Step 4: Update the channel drawer**

In `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`:

1. Remove the whole `{currentType === CHANNEL_TYPE_SUB2API && (<> ... </>)}` block (lines ~4204-4410) that contains the codex compat section and the `chat_completions_to_responses` toggle. Also remove the now-unused `currentCodexCompatEnabled` watch (line ~779) and any `CHANNEL_TYPE_SUB2API`-only logic added for this feature.
2. Inside the `{currentType === CHANNEL_TYPE_ADVANCED_CUSTOM && (...)}` block, right after the "Advanced Custom Routes" FormField (around line 2919), add the new compat section:

```tsx
{currentType === CHANNEL_TYPE_ADVANCED_CUSTOM && (
  <div className='space-y-4 border-t px-4 py-3'>
    <FormField
      control={form.control}
      name='codex_compat_enabled'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Simulate Codex client')}</FormLabel>
            <FormDescription>
              {t(
                'Spoof official Codex CLI identity (User-Agent, originator) for upstreams that gate on the Codex client family. Client-provided session, thread, and x-codex-* headers are passed through unchanged.'
              )}
            </FormDescription>
          </div>
          <FormControl>
            <Switch
              checked={field.value === true}
              onCheckedChange={(checked) => {
                field.onChange(checked)
                if (checked && !form.getValues('codex_client_version')?.trim()) {
                  form.setValue('codex_client_version', DEFAULT_CODEX_CLIENT_VERSION, {
                    shouldDirty: true,
                    shouldValidate: true,
                  })
                }
              }}
            />
          </FormControl>
        </FormItem>
      )}
    />

    {form.watch('codex_compat_enabled') === true && (
      <>
        <FormField
          control={form.control}
          name='codex_client_version'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Codex client version')}</FormLabel>
              <FormControl>
                <Input placeholder={DEFAULT_CODEX_CLIENT_VERSION} {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Codex client version must start with X.Y.Z (e.g. 0.146.0)'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='codex_client_name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Codex client name')}</FormLabel>
              <FormControl>
                <Input placeholder='codex_cli_rs' {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Optional. Defaults to codex_cli_rs; used as the User-Agent client segment and originator.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </>
    )}

    <FormField
      control={form.control}
      name='claude_compat_enabled'
      render={({ field }) => (
        <FormItem className='flex items-center justify-between'>
          <div className='space-y-0.5'>
            <FormLabel>{t('Simulate Claude Code client')}</FormLabel>
            <FormDescription>
              {t(
                'Shape Claude Messages requests like the official Claude Code CLI: claude-cli User-Agent, anthropic-beta claude-code-20250219, x-app: cli, and the Claude Code identity line as the first system block. Enable when the gateway only accepts Claude Code clients.'
              )}
            </FormDescription>
          </div>
          <FormControl>
            <Switch
              checked={field.value === true}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </FormItem>
      )}
    />
  </div>
)}
```

Remove the now-unused `CODEX_IDENTITY_MODE_*` imports/consts from the drawer (lines ~156-158) if nothing else uses them, and remove the `{t('Codex compatibility')}`, `{t('Codex identity mode')}`, and `chat_completions_to_responses` i18n usages from the drawer.

- [ ] **Step 5: i18n keys**

In `web/src/i18n/locales/en.json` add keys (all languages must have equivalents after `bun run i18n:sync`; add to en, zh, zh-TW, fr, ru, ja, vi with proper translations):

- `Simulate Codex client`
- `Spoof official Codex CLI identity (User-Agent, originator) for upstreams that gate on the Codex client family. Client-provided session, thread, and x-codex-* headers are passed through unchanged.`
- `Simulate Claude Code client`
- `Shape Claude Messages requests like the official Claude Code CLI: claude-cli User-Agent, anthropic-beta claude-code-20250219, x-app: cli, and the Claude Code identity line as the first system block. Enable when the gateway only accepts Claude Code clients.`
- `Optional. Defaults to codex_cli_rs; used as the User-Agent client segment and originator.`

Remove keys that are no longer referenced: the old "Codex compatibility" description ("Shape Sub2API requests like official Codex CLI..."), "Codex identity mode", and the `chat_completions_to_responses` label/description keys.

Run from `web/`: `bun run i18n:sync`. Follow the project `i18n-translate` skill for the locale files (zh/zh-TW/fr/ru/ja/vi translations).

- [ ] **Step 6: Type-check and build**

Run from `web/`:
```bash
bun install
bun run i18n:sync
bunx tsc --noEmit
bun run build
```
Expected: type-check clean, build succeeds. Also grep to confirm no lingering references: `grep -rn "codex_identity_mode\|chat_completions_to_responses" web/src/features/channels/` should return nothing (the global `chat_completions_to_responses_policy` in `web/src/features/system-settings/` must remain untouched).

- [ ] **Step 7: Commit (controller)**

```bash
git add web/src/features/channels web/src/i18n/locales
git commit -m "feat(web): advanced custom simulate codex/claude-code client settings"
```

---

## Self-Review (controller runs before execution)

- **Spec coverage:** Spec §2.1 (codex headers, no synthesis) → Task 3 + Task 4. §2.2 (claude headers + system identity, idempotent) → Task 3 + Task 4. §4.4 removals → Tasks 1 + 2. §5 frontend → Task 4. §6 tests → each task's test steps. §7 naming → Task 3/4 constants and labels.
- **No placeholders:** every task has concrete code, test commands, and expected outcomes.
- **Type consistency:** `applyCodexIdentityHeaders`/`applyClaudeCodeHeaders`/`prependClaudeCodeSystemPrompt` signatures are defined once in Task 3 and used in the same task. Field names `codex_compat_enabled` / `claude_compat_enabled` are identical across Go DTO and TS type.
