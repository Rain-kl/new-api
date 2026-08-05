# Sub2API Codex Compatibility Design

**Date:** 2026-08-05  
**Status:** Approved (revised after cross-review)  
**Scope:** new-api only (no sub2api code changes)  
**Supersedes:** earlier “new Codex Gateway channel type” draft

## 1. Problem

Operators route traffic through new-api to **sub2api** using a sub2api API key and custom base URL. When the selected sub2api upstream account enables `codex_cli_only`, requests must look like the **official Codex client family**.

Existing new-api pieces:

| Channel | What it does | Gap |
|---------|--------------|-----|
| **Codex (57)** ChatGPT Subscription | OAuth → `chatgpt.com/backend-api/codex/*`; sets `originator`, not full official UA / `x-codex-*` | Wrong upstream target for sub2api |
| **Sub2API (59)** | Bearer API key + custom base; paths mostly passthrough (`/v1/responses`, compact, …) | **No** Codex identity synthesis / sticky session keys |

**Decision (post-review):** Do **not** add a parallel channel type. **Enhance Sub2API (59)** with an optional Codex-compat layer.

## 2. Goals and non-goals

### Goals

1. Optional **Codex compatibility mode** on Sub2API channels.
2. Outbound auth remains `Authorization: Bearer <channel.key>` (sub2api API key).
3. Paths remain OpenAI-compatible: `{base}/v1/responses`, `{base}/v1/responses/compact` (and existing alpha/search as today).
4. Dual identity behavior:
   - **Preserve** real Codex CLI headers and body cache keys when inbound identity can pass sub2api’s official-client gate.
   - Otherwise **synthesize** a minimal official identity with **stable** `session_id` / `thread_id` / `x-codex-window-id` and stable body `prompt_cache_key`.
5. Never use pure per-request random IDs for session/thread/window/cache key when synthesizing.
6. Treat body **`prompt_cache_key` as first-class** for multi-turn prefix cache; headers support the gate and CLI parity.
7. Keep default Sub2API behavior **unchanged** when the feature is off.
8. Leave ChatGPT Subscription Codex (57) behavior **unchanged**.

### Non-goals

- ChatGPT OAuth / `chatgpt-account-id` on this path.
- Primary use of `/backend-api/codex/*` from new-api (sub2api accepts it, but Sub2API channel keeps v1 path style).
- TLS / JA3 impersonation.
- Auto-fetch latest Codex CLI version.
- Full synthetic recreation of every optional Codex header when absent.
- Changing sub2api server code.
- Using `previous_response_id` as the primary sticky key (still forwarded in body if present).

## 3. Background: sub2api `codex_cli_only`

Account flag: OpenAI OAuth account `extra.codex_cli_only`. Checked on **inbound** to sub2api in `Forward` before upstream.

Default gate (simplified):

1. Flag off → no restriction.
2. Optional global force bypass.
3. Blacklist → deny.
4. Identity: official UA (strict prefixes) **or** official originator **or** whitelist **or** app-server allow.
5. If official UA/originator candidate: UA must parse engine version `X.Y.Z`; optional min/max.
6. Engine fingerprint: all Required signals. Default seed requires at least one header name prefix `x-codex-` (e.g. `x-codex-window-id`). `session_id` / `thread_id` optional by default.

Implications:

- `originator` alone + `User-Agent: Go-http-client/1.1` → **fail** (version undetectable + missing fingerprint).
- Fingerprint checks **presence**, not UUID schema.
- Random per-request IDs may pass the gate but **break** prompt-cache partitions.

### sub2api follow-on behavior (do not fight blindly)

- OAuth outbound may **isolate** `session_id` with `isolateOpenAISessionID(apiKeyID, …)` and derive from `prompt_cache_key`.
- Header whitelist to ChatGPT includes `user-agent`, `originator`, `session_id`, `x-codex-window-id`, turn headers; **does not include `thread_id`**.
- OAuth passthrough: for models whose name contains `codex`, **empty-string `instructions` is rejected (403)**; missing `instructions` may be filled with a long default system prompt.
- Compact: unary JSON; prefer `Accept: application/json`; may map compact models.

## 4. Background: real Codex session semantics

Within one CLI thread:

| Field | Role |
|-------|------|
| `thread_id` | Stable thread UUID (header) |
| `prompt_cache_key` | Body key; typically **same string as thread id**; provider cache partition |
| `session_id` | Stable session id |
| `x-codex-window-id` | Window/engine fingerprint |
| `originator` + `User-Agent` | Must be paired (same client family / leading name) |
| Turn headers | May change; client re-sends server echoes |

Cache hit = same partition key **and** byte-identical prompt prefix.

## 5. Architecture

```text
Client
  ├─ Real Codex CLI  → preserve identity when gate-capable
  └─ Other client    → synthesize sticky official identity
        │
        ▼
new-api  ChannelTypeSub2API (59)
  codex_compat enabled?
    no  → existing newapi.Adaptor behavior only
    yes → codex identity layer + optional body patches
  Auth: Bearer <api key>
  URL:  {base}{request path}  (e.g. /v1/responses, /v1/responses/compact)
        │
        ▼
sub2api (API key auth + optional codex_cli_only on OAuth accounts)
        │
        ▼
Upstream account pool
```

### Package layout

| Unit | Responsibility |
|------|----------------|
| `relay/channel/codexcompat/` (new) | Pure helpers: official detection, sticky ID derivation, header apply, version validation. No Gin dependency preferred for unit tests. |
| `relay/channel/sub2api/` | Override `SetupRequestHeader` / `ConvertOpenAIResponsesRequest` when compat on; embed `newapi.Adaptor` otherwise. |
| `relaykit/dto.ChannelSettings` | Persist compat flags/version. |
| Frontend channel form | Toggle + version field for type 59. |

Do **not** invent `ChannelTypeCodexGateway`.

## 6. Configuration

On `ChannelSettings` (channel JSON settings), for Sub2API:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `codex_compat_enabled` | bool | `false` | Master switch |
| `codex_client_version` | string | e.g. `0.146.0` | Required when enabled; must match `X.Y.Z` (optional pre-release suffix policy: require numeric triple at least) |
| `codex_client_name` | string | `codex_cli_rs` | Synthetic originator / UA client segment |
| `codex_identity_mode` | string | `auto` | `auto` \| `passthrough` \| `synthesize` |

Validation (save and/or first request):

- If `codex_compat_enabled` and mode is `auto` or `synthesize`, `codex_client_version` must be non-empty and parseable as engine version for UA construction.
- Invalid mode string → error.

Channel key remains a plain API key (not OAuth JSON).

## 7. Outbound URL and auth

Unchanged from current Sub2API:

- `GetRequestURL`: base + request path (alpha search special-case already exists).
- `Authorization: Bearer <key>`.
- No `chatgpt-account-id`.

When compat enabled, still force `Content-Type: application/json` for Responses (avoid charset rejection on some gateways).

### Compact Accept

When `RelayModeResponsesCompact` (path `/v1/responses/compact`):

- Set outbound `Accept: application/json` (do not leave SSE Accept from client).

When normal streaming Responses:

- `Accept: text/event-stream` if stream; else `application/json` if empty.

## 8. Identity policy

### 8.1 Gate-capable inbound detection

Port logic aligned with sub2api (reimplement; do not import sub2api):

- Official UA strict prefixes (`codex_cli_rs/`, `codex-tui/`, `codex_vscode/`, …) **and** parseable `X.Y.Z` from UA, **or**
- Official originator **and** parseable `X.Y.Z` from UA (originator alone is **not** enough for passthrough-without-fix).

`IsGateCapableOfficialCodex(ua, originator) bool`:

```text
true only if:
  (strict official UA OR official originator)
  AND ParseCodexEngineVersion(ua) succeeds
```

Optional: also require at least one inbound `x-codex-*` when we want to mirror default fingerprint strictly for passthrough; if missing, **upgrade** headers (synthesize fingerprint) while preserving session/thread/cache keys (see 8.3).

### 8.2 Mode resolution

```text
mode = settings.codex_identity_mode
if mode == "passthrough":
  use preserve path only (still fill missing gate-critical headers if needed — see below)
if mode == "synthesize":
  always synthesize UA/originator/window at least; sticky IDs per 8.4
if mode == "auto" (default):
  if IsGateCapableOfficialCodex(ua, originator):
    preserve path
  else:
    synthesize path (with ID preserve when present)
```

**Critical fix vs first draft:** never pure-passthrough a request that would still fail sub2api version/fingerprint checks.

### 8.3 Preserve path (gate-capable or partial)

Forward when present (case-insensitive):

- `User-Agent`, `originator`
- `session_id` / `session-id`
- `thread_id` / `thread-id`
- all inbound `x-codex-*` (at least window-id, turn-state, turn-metadata, beta-features, installation-id)

Rules:

- Do not replace client session/thread/window with new values.
- Do not rewrite body `prompt_cache_key` if set.
- If gate-capable UA+originator but **no** `x-codex-*` header: inject sticky `x-codex-window-id` only (keep other IDs).
- If originator and UA leading name conflict, prefer pairing fix only when synthesizing; in preserve path leave client values if already gate-capable.

Auth always channel API key.

### 8.4 Synthesize path

Emit at least:

```http
User-Agent: {codex_client_name}/{codex_client_version} ({os}; {arch})
originator: {codex_client_name}
session_id: <sticky>
thread_id: <sticky>
x-codex-window-id: <sticky>
```

UA must make `ParseCodexEngineVersion` succeed (version segment = configured version).

OS/arch may be static (`linux; x86_64`) for determinism.

### 8.5 Sticky ID resolution (synthesize / fill-missing)

Order:

1. Inbound `thread_id` / `thread-id` if set.
2. Else body `prompt_cache_key` if non-empty string.
3. Else inbound `session_id` / `session-id` if set (use as thread seed).
4. Else deterministic UUID-like ids from seed:

   ```text
   seed = fmt.Sprintf("%d|%d|%d", channel_id, user_id, token_id)
   thread_id  = UUID5(ns_thread, seed)
   session_id = UUID5(ns_session, seed)   // may equal thread for simplicity
   window_id  = UUID5(ns_window, seed)
   ```

5. Body: if `prompt_cache_key` missing or empty → set to **thread_id string**.

**Never** `uuid.New()` per request for these fields.

Keep IDs short (UUID string form, 36 chars) to avoid known upstream length issues (~64).

**Limitation:** token-scoped fallback merges concurrent chats on one token. Document in UI. Future: optional inbound `X-Client-Thread-Id` in seed.

### 8.6 `previous_response_id`

- Official Responses body field; forward if present.
- Not used as primary sticky map for this design (Codex-first).

## 9. Body shaping (compat on only)

### Responses (non-compact)

- Prefer **not** forcing `instructions: ""` (differs from channel 57).  
  - If client omitted instructions: leave omitted (sub2api may inject defaults for codex models).  
  - If client sent non-empty: keep.  
  - If client sent empty string and model name contains `codex`: either strip field or leave to operator; **do not document empty string as required**. Prefer **delete empty instructions** when compat on to avoid sub2api passthrough 403.
- Optional: do **not** force `store=false` by default for Sub2API (sub2api/account policies vary).  
  - Prefer: only apply store=false when settings flag `codex_force_store_false` is true (default **false** for Sub2API path).  
  - YAGNI: omit force-store unless product later requires ChatGPT-parity.

MVP body work when compat on:

1. Ensure sticky `prompt_cache_key` injection (8.5).  
2. Remove empty-string `instructions` for safety with sub2api.  
3. No aggressive field stripping beyond that unless pass-through body is off and existing global RemoveDisabledFields already runs.

### Compact (`/v1/responses/compact`)

- Same identity / sticky / `prompt_cache_key` policy as Responses (shared helpers).
- Outbound `Accept: application/json`.
- Do **not** apply non-compact-only mutations.
- Compact is **context compression**, same conversation identity as prior turns.

## 10. Response handling

Reuse existing OpenAI Responses / compaction handlers via embedded `newapi.Adaptor` → openai adaptor.

When compat on, ensure SSE/response path still copies useful Codex headers to client if present (existing `copyCodexSSEHeaders` in stream path applies when response goes through that helper).

## 11. Interaction with header overrides / affinity

Order (later wins where appropriate):

1. Base SetupApiRequestHeader (Content-Type/Accept from client).
2. Sub2API auth Bearer.
3. Codex compat identity apply.
4. Channel header override / runtime override (admin still highest for explicit overrides).

Channel affinity `codex cli trace` remains optional ops tooling; compat layer must work **without** affinity. If both run, overrides after adaptor still win.

## 12. Frontend / admin UX

Sub2API channel form:

- Switch: “Codex compatibility (official client headers)”
- Fields when on: client version (required), client name (optional), identity mode (auto/passthrough/synthesize)
- Help text:
  - Real Codex: point CLI at new-api; gate-capable identity is preserved.
  - Other clients: synthetic identity; session keys sticky per user token by default.
  - Requires upstream sub2api account may use `codex_cli_only`.

i18n: English keys + locale sync.

## 13. Testing

### Unit (`codexcompat` + `sub2api`)

- Gate-capable true/false tables (UA/originator/version).
- Auto mode: weak originator-only → synthesize UA with version.
- Preserve: session/thread/window/prompt_cache_key unchanged.
- Synthesize: same seed → same IDs; `prompt_cache_key == thread_id` when missing.
- Compact: Accept application/json; same sticky ids as responses for same seed.
- Compat off: headers equal to pre-change Sub2API (Bearer only + SetupApiRequestHeader).
- Empty instructions string removed when compat on.

### Manual / integration

Against sub2api with `codex_cli_only` + default fingerprint:

1. curl without compat → 403 (if flag on).  
2. curl with compat on → 200.  
3. Real Codex CLI via new-api → 200; IDs preserved.  
4. Two turns same token → same `prompt_cache_key`.  
5. Compact then responses → same cache key.

## 14. Risks and limitations

| Risk | Mitigation |
|------|------------|
| sub2api tightens fingerprints | Minimal set matches current default; extend settings later |
| Token sticky merges chats | Document; optional client thread header later |
| sub2api isolates session_id per API key | Expected multi-tenant isolation; stable **input** keys still help routing/cache |
| Official client list drift | Unit tests; comment sub2api reference date |
| Bypassing “official only” policy | Operator-controlled self-host chaining |
| Confusion with channel 57 | UI copy: Sub2API ≠ ChatGPT Subscription |

## 15. Decisions log

| Decision | Choice |
|----------|--------|
| Product surface | Enhance **Sub2API (59)**, no new channel type |
| Default | `codex_compat_enabled=false` (zero behavior change) |
| Path | Keep `/v1/responses` style via existing Sub2API URL logic |
| Passthrough criterion | Gate-capable = official identity **and** parseable UA version |
| Sticky | prompt_cache_key / inbound ids / token UUID5; never per-request random |
| Cache first-class | body `prompt_cache_key` |
| instructions | Do not force `""`; strip empty string when compat on |
| store=false | Not forced on Sub2API MVP |
| previous_response_id | Forward only |
| compact | Same identity; Accept JSON |

## 16. Success criteria

1. Sub2API with compat **off**: identical outbound behavior to today.  
2. Compat **on**, synthetic client → sub2api `codex_cli_only` default policy accepts request.  
3. Compat **on**, real Codex gate-capable inbound → session/thread/window/`prompt_cache_key` preserved.  
4. Two sequential synthetic requests same user/token → identical sticky ids and `prompt_cache_key`.  
5. Compact + responses share sticky policy; compact uses Accept application/json.  
6. Channel 57 ChatGPT Codex unchanged.

## 17. Implementation outline

1. Add `codexcompat` package (detect, sticky, apply headers).  
2. Extend `ChannelSettings` + validation.  
3. Sub2API adaptor overrides when enabled.  
4. Frontend form + i18n.  
5. Tests as §13.  
6. Optional short operator note in channel hint text.
