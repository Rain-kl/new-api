# Channel Reasoning Effort Rules Design

**Date:** 2026-08-06  
**Status:** Approved for spec (brainstorming)  
**Scope:** Channel-level per-model reasoning effort configuration  
**Related:** `ChannelSettings`, model mapping (`OriginModelName` vs `UpstreamModelName`), OpenAI/Anthropic effort fields

## 1. Problem

Operators want to pin a **reasoning effort / intensity** per channel model (or model-mapping alias) without requiring every client to pass the correct field.

Typical use case:

1. Channel publishes model `gpt-pro`.
2. Operator adds model mapping: `gpt-pro[max]` → `gpt-pro`.
3. Operator enables channel reasoning rules: for `gpt-pro[max]`, effort = `max`, **force override**.
4. Any client request with `model=gpt-pro[max]` is sent upstream with max reasoning effort, even if the client omitted (or sent a weaker) effort field.

Today, effort only comes from:

- Client protocol fields (`reasoning_effort`, `reasoning.effort`, Claude `output_config.effort` / `thinking`)
- Model-name suffixes (`-high`, `-max`, …) parsed in OpenAI / Claude paths
- Ad-hoc `param_override` (no first-class per-model force semantics)

There is no channel UI to declare “this client-facing model always (or by default) uses effort X”.

## 2. Goals and non-goals

### Goals

1. Channel setting: multi-rule list of `{ model, effort, force }`, with a master enable switch.
2. Match on **client-facing model name** (`OriginModelName`), including model-mapping **source** keys.
3. Force semantics:
   - `force=true`: always write channel effort; ignore client protocol effort.
   - `force=false`: write channel effort only when client did **not** specify a protocol-native effort field.
4. Apply only on these protocols:
   - OpenAI Chat Completions
   - OpenAI Responses
   - Anthropic Messages
5. Place logic at the **protocol handler layer** (after model mapping, before channel adaptor conversion), not inside each channel type.
6. Frontend: enable switch + editable rule rows; model options = channel models ∪ mapping sources; effort = preset combobox + free custom text.
7. When a channel rule **actually applies**, subsequent model-suffix effort parsing must not overwrite the applied effort (suffix may still strip the model name).

### Non-goals

- Gemini / other protocols (out of scope for this feature).
- Changing billing formulas based on effort.
- Auto-deriving Claude `thinking.type` / budget tokens from effort (only write effort string into the protocol field).
- Making the feature work under global/channel **body pass-through** (DTO is not rebuilt; same limitation as system prompt injection).
- Regex / wildcard model matching (exact match only).

## 3. Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Layer | Protocol handlers after `ModelMappedHelper` | One implementation covers all channel types that use Chat / Responses / Messages; no per-adaptor edits for Azure/custom OpenAI-compatible |
| Match key | `info.OriginModelName` exact match | Mapping aliases (`gpt-pro[max]`) are client model names; upstream name is already remapped separately |
| Storage | `ChannelSettings` JSON | Same place as `system_prompt`, `force_format`; no DB migration |
| Effort values | Free string + UI presets | OpenAI/Anthropic add levels over time; presets are UX only |
| Client “specified” | Protocol fields only | Model suffixes are not client effort for force=false decisions |
| Suffix interaction | If channel rule applied, suffix must not overwrite effort | Avoid force=true being undone by `-high` on upstream name |
| Channel type filter | No backend type filter | Workload assessment: shared handlers, not per-channel adaptors |
| Pass-through body | Feature inactive | Consistent with other DTO-level channel settings |

## 4. Data model

### 4.1 `ChannelSettings` fields

```go
// Master switch. When false, ReasoningEffortRules is ignored.
ReasoningEffortRulesEnabled bool `json:"reasoning_effort_rules_enabled,omitempty"`

// Per-model rules. Matched against OriginModelName (client model / mapping source).
ReasoningEffortRules []ReasoningEffortRule `json:"reasoning_effort_rules,omitempty"`
```

```go
type ReasoningEffortRule struct {
	// Model is the client-facing model name: a published channel model
	// or a model-mapping source key (e.g. "gpt-pro[max]").
	Model string `json:"model"`
	// Effort is written into the protocol effort field as-is (e.g. "max", "high").
	Effort string `json:"effort"`
	// Force when true always overwrites client effort; when false only fills if client omitted.
	Force bool `json:"force,omitempty"`
}
```

### 4.2 Example `setting` JSON

```json
{
  "reasoning_effort_rules_enabled": true,
  "reasoning_effort_rules": [
    {
      "model": "gpt-pro[max]",
      "effort": "max",
      "force": true
    },
    {
      "model": "gpt-pro",
      "effort": "high",
      "force": false
    }
  ]
}
```

### 4.3 Validation (save-time)

On channel create/update (backend), when `reasoning_effort_rules_enabled` is true or rules are non-empty:

| Rule | Error |
|------|-------|
| `model` empty after trim | reject |
| `effort` empty after trim | reject |
| duplicate `model` (case-sensitive, after trim) | reject |

Frontend mirrors the same checks before submit.

No whitelist on `effort` content.

### 4.4 RelayInfo flag

```go
// ReasoningEffortFromChannel is true when a channel reasoning rule was applied
// for this request (force or default-fill). Adaptor suffix parsers must not
// overwrite the effort field when this is set; they may still strip suffixes
// from the upstream model name.
ReasoningEffortFromChannel bool
```

Also call existing `info.SetReasoningEffort(effort)` when applying.

## 5. Runtime flow

```
Client request  model = "gpt-pro[max]",  (maybe reasoning_effort omitted)
  → auth / distributor selects channel
  → RelayInfo.OriginModelName = "gpt-pro[max]"
  → ModelMappedHelper
       mapping: gpt-pro[max] → gpt-pro
       UpstreamModelName = "gpt-pro"
  → ★ ApplyChannelReasoningEffortRules(info, request)
       lookup OriginModelName in rules
       apply force / default-fill on protocol fields
  → adaptor Convert*Request
       if ReasoningEffortFromChannel: do not let model suffix overwrite effort
       suffix may still peel UpstreamModelName
  → upstream
```

### 5.1 Lookup

1. If `!ChannelSetting.ReasoningEffortRulesEnabled` → return.
2. If rules empty → return.
3. Trim `OriginModelName`; find first rule whose trimmed `model` equals it.
4. If not found or rule `effort` empty → return.
5. Decide apply:
   - if `force` → apply
   - else if client has protocol-native effort → skip
   - else → apply
6. On apply: write protocol field, `SetReasoningEffort`, set `ReasoningEffortFromChannel = true`.

### 5.2 “Client has protocol-native effort”

| Protocol | Already specified when |
|----------|------------------------|
| Chat Completions | `strings.TrimSpace(request.ReasoningEffort) != ""` |
| Responses | `request.Reasoning != nil && strings.TrimSpace(request.Reasoning.Effort) != ""` |
| Anthropic Messages | `GetEfforts() != ""` **or** (`Thinking != nil` and `Thinking.Type != ""`) |

Model-name suffixes (`-high`, `-max`, …) do **not** count as client-specified for this decision.

### 5.3 Protocol writes

| Protocol | Write |
|----------|-------|
| Chat Completions | `request.ReasoningEffort = effort` |
| Responses | Ensure `request.Reasoning != nil`; set `request.Reasoning.Effort = effort` (preserve `Summary` / other fields) |
| Anthropic Messages | Merge into `output_config` JSON so `effort` is set, without dropping other `output_config` keys |

Claude: do **not** auto-create `thinking` objects solely for this feature.

### 5.4 Call sites

| Handler | When |
|---------|------|
| `relay/compatible_handler.go` (Chat Completions path) | After model mapping / before convert; also on chat→responses path after system prompt, before conversion |
| `relay/responses_handler.go` | After model mapping, before convert / param override |
| `relay/claude_handler.go` | In or immediately after `prepareClaudeRequest` / after model mapping, before convert |

Shared implementation lives in `relay/common` or `relay/helper` (single package, table-tested). Prefer pure functions that take `ChannelSettings` + origin model + current client effort presence, return `(effort string, applied bool)` plus small per-protocol wrappers that mutate DTOs.

### 5.5 Adaptor suffix protection

OpenAI adaptor today:

```go
effort, originModel := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(info.UpstreamModelName)
if effort != "" {
    request.ReasoningEffort = effort  // must skip overwrite when FromChannel
    ...
}
```

Change: when `info.ReasoningEffortFromChannel`, still peel suffix from model name if present, but **do not** assign suffix effort over the request field (and keep `info.ReasoningEffort` as channel value).

Claude `prepareClaudeRequest` suffix / `-thinking` paths: if `ReasoningEffortFromChannel` already true, do not overwrite `output_config.effort` from suffix; model name cleanup may still run as today when suffix was part of the model string.

If Claude suffix logic runs **before** channel rules in the same function, reorder so:

1. Channel rules apply (using OriginModelName / request.Model before suffix strip as appropriate — **match key is always OriginModelName from RelayInfo**, not the possibly rewritten request.Model).
2. Existing Claude model-suffix adapters run afterward with “do not overwrite effort if FromChannel”.

### 5.6 Pass-through body

When `PassThroughRequestEnabled` (global) or `ChannelSetting.PassThroughBodyEnabled`, handlers skip DTO rebuild. Channel reasoning rules **do not apply**. Document in UI help text.

## 6. Frontend

### 6.1 Placement

Channel mutate drawer, Advanced Settings area (near system prompt / format toggles). Available for **all channel types** with helper text:

> Applies only to Chat Completions, Responses, and Anthropic Messages. Inactive when request body pass-through is enabled.

### 6.2 Controls

1. Switch: **Enable custom reasoning effort** → `reasoning_effort_rules_enabled`
2. When on, rule list:
   - **Model**: combobox/select options = unique union of:
     - models from channel `models` field (comma-separated list)
     - keys from `model_mapping` JSON object
     - plus free text for manual entry
   - **Effort**: combobox presets `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` + allow arbitrary string
   - **Force override**: checkbox; help: when checked, always use this effort; when unchecked, prefer client effort if present
   - Remove row / Add rule

### 6.3 Form plumbing

Mirror existing channel setting fields (`force_format`, `system_prompt`, …):

- `web/src/features/channels/types.ts` — `ChannelSettings` types
- `web/src/features/channels/lib/channel-form.ts` — zod, defaults, parse/build setting JSON
- `channel-mutate-drawer.tsx` (+ optional small editor component)
- i18n English keys via `t('...')`; complete locales with project i18n skill/tooling

### 6.4 Validation UX

- Disable submit or surface form errors for empty model/effort and duplicate models.
- Empty rules with switch on: allowed (no-op at runtime) or warn — prefer **allow** (switch on, empty list = no-op) to avoid save friction.

## 7. Error handling and observability

| Scenario | Behavior |
|----------|----------|
| Switch off | No-op |
| No matching model | No-op |
| Applied successfully | DTO updated; `ReasoningEffort` on RelayInfo set |
| Invalid effort for upstream | Upstream error returned as today |
| Pass-through body | Silent no-op |
| Invalid settings on save | 400 with clear validation message |

Optional debug log when applied (behind existing debug flags only): origin model, effort, force.

## 8. Testing

Backend table tests (testify require/assert):

1. **Mapping alias force:** Origin `gpt-pro[max]`, rule force max → Chat `ReasoningEffort == "max"`; Upstream remains mapped model.
2. **force=false with client effort:** client `high`, rule `max` force false → remains `high`.
3. **force=false without client effort:** empty client → filled with rule effort.
4. **force=true overwrites client.**
5. **Responses** path writes `reasoning.effort`.
6. **Claude** merges `output_config.effort` without wiping other keys.
7. **Switch off** ignores rules.
8. **Duplicate model validation** on settings validate helper.
9. **Suffix protection:** FromChannel true + upstream name with `-high` → model stripped, effort stays channel value.

Frontend: form serialize/deserialize round-trip for the new fields (existing channel-form test style if present).

## 9. Implementation outline (for writing-plans)

1. DTO + validation in `relaykit/dto/channel_settings.go` (+ tests).
2. Pure apply helper + RelayInfo flag.
3. Wire Chat / Responses / Claude handlers.
4. OpenAI (+ Claude) suffix protection.
5. Frontend types, form, UI, i18n.
6. Regression tests as in §8.

## 10. Worked example

| Config piece | Value |
|--------------|-------|
| Channel models | `gpt-pro` |
| Model mapping | `{"gpt-pro[max]":"gpt-pro"}` |
| Rule | model=`gpt-pro[max]`, effort=`max`, force=`true` |

| Client request | Upstream model | Upstream effort |
|----------------|----------------|-----------------|
| `model=gpt-pro[max]`, no `reasoning_effort` | `gpt-pro` | `max` |
| `model=gpt-pro[max]`, `reasoning_effort=low` | `gpt-pro` | `max` (forced) |
| `model=gpt-pro`, no effort, no rule for `gpt-pro` | `gpt-pro` | (unchanged / none) |
| `model=gpt-pro` with rule force=false effort=high, client empty | `gpt-pro` | `high` |
| `model=gpt-pro` with rule force=false effort=high, client `low` | `gpt-pro` | `low` |
