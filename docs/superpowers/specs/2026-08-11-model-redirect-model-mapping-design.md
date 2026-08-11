# Model Redirect: Global Model-Mapping Mode (模型映射)

Date: 2026-08-11
Status: Approved design (pending spec review)

## Goal

Add a **model-mapping mode** to the existing `/models/redirect` (模型重定向)
virtual-model feature. Today a virtual model distributes to **specific channels**
by priority; every channel change forces an admin edit. The new mode lets a
virtual model map its name to a **target model name** (e.g. `auto` → `gpt-5.6`)
and then hand routing to the channel layer — no channel list in the entry. Each
virtual model independently chooses `redirect` or `mapping` mode, and switching
mode never destroys the other mode's configuration.

The two modes compose into one recursive virtual-model graph: a redirect entry
can priority-reference mapping entries, a mapping target can itself be a virtual
model (redirect or mapping), and resolution terminates only when the result is a
real model name (routed normally) or a concrete channel hop.

## Decisions (confirmed with requester)

1. **Per-entry mode, not a global switch.** Each virtual model has its own
   `mode` (`redirect` | `mapping`). A single virtual model is never both.
2. **Mode switch is non-destructive.** Both configs are stored forever. Editing
   an entry in one mode never deletes the other mode's stored config; the backend
   update only writes the active mode's fields. Users can switch any time.
3. **Mapping targets are recursive, not terminal.** After mapping (or redirect),
   the resulting model name is re-checked against the virtual model list:
   - still a **redirect** model → resolve via the existing redirect priority logic;
   - still a **mapping** model → keep mapping until the result leaves the list;
   - not in the list → normal channel routing (as if the client requested that name).
   Cycle-safe (depth limit + visit stack at runtime; validation rejects mapping
   cycles at save time).
4. **Mapped hops reuse existing routing semantics.** A "model-only" hop behaves
   exactly like a normal request for the mapped model: channel selection via
   `GetRandomSatisfiedChannel`, per-channel-priority-tier retries within the
   `RetryTimes` budget, the channel's own `model_mapping` still applies on top.
5. **Nested composition works both ways.** `redirect → mapping` (priority chain
   over mapping entries), `mapping → redirect`, and `mapping → mapping` chains
   are all supported by the single recursive resolver.

## Terminology

- **Virtual model** — a `ModelRedirect` row (virtual name bound to groups).
- **Redirect entry** (`mode = redirect`) — owns a priority chain of `Targets`
  (channel hop or nested reference to another virtual model). Existing behavior.
- **Mapping entry** (`mode = mapping`) — owns a single `MappingTarget` model
  name. No channel chain.
- **Channel-bound candidate** — `RedirectCandidate{ChannelID > 0, Model}`.
- **Model-only candidate** — new candidate kind: `RedirectCandidate{ChannelID == 0, Model != ""}`.
  The channel is chosen by the channel layer at pick time for `Model`.

## Architecture / data flow

### 1. Data model (`model/model_redirect.go`)

`ModelRedirect` gains two columns (AutoMigrate via `RegisterMainDBModel`; safe on
SQLite/MySQL/PostgreSQL):

```go
Mode          string `json:"mode"           gorm:"size:16;default:redirect"` // "redirect" | "mapping"
MappingTarget string `json:"mapping_target" gorm:"size:128;default:''"`      // mapping-mode target model
```

Constants: `ModelRedirectModeRedirect = "redirect"`, `ModelRedirectModeMapping = "mapping"`.

### 2. Input / validation / CRUD

- `ModelRedirectInput` gains `Mode string` and `MappingTarget string`.
- `ModelRedirectTargetInput` unchanged.
- Validation (`validateModelRedirectInput`):
  - `Mode` empty → `"redirect"` (backward compatible); must otherwise be one of
    the two constants.
  - **redirect mode**: existing target validation (≥1 enabled target, channel
    existence, nested-cycle detection).
  - **mapping mode**: `MappingTarget` required, ≤ 128 chars, no commas / control
    whitespace. Targets not required and not validated. If the target names an
    existing virtual model, that virtual model **must be enabled** (mirrors the
    redirect sentinel requirement).
  - **Cycle detection extended**: mapping-mode `MappingTarget` values become
    graph edges in `buildModelRedirectNestedEdgeMap` (in addition to redirect
    sentinel refs). `a→b→a` mapping cycles and self-reference `a→a` are rejected
    at save time.
- `CreateModelRedirect`: mapping mode creates the row with no targets; redirect
  mode as today.
- `UpdateModelRedirect` — **non-destructive mode contract**:
  - mode = `redirect`: update name/groups/enabled/remark/mode; **replace
    targets** (today's tx delete-and-recreate); `mapping_target` preserved
    untouched.
  - mode = `mapping`: update name/groups/enabled/remark/mode/`mapping_target`;
    **targets preserved untouched** (no delete/recreate).
- `DeleteModelRedirect` / status toggle: unchanged.

### 3. Cache (`buildModelRedirectCacheMap`)

`modelRedirectCacheEntry` gains `Mode string` and `MappingTarget string`.

- **mapping entry** (enabled, non-empty groups, non-empty target) →
  `{Mode: mapping, MappingTarget: target, Groups: set}` (no targets).
- **redirect entry** → today's entry with `Mode: redirect`.

Cache invalidation: unchanged (`InvalidateModelRedirectCache`).

### 4. Recursive resolution (`ResolveModelRedirect` + new resolver)

`ResolveModelRedirect(clientModel, usingGroup)` keeps its entry-point contract:
top-level lookup in the cache; **not a virtual model → `ok=false`** (normal
routing untouched). When the top-level name is virtual, a new recursive resolver
produces the candidate list:

```
resolve(name, group, stack, depth, priorityOverride):
  entry = virtual[name]                      // cache lookup
  if entry.mode == mapping:
    target = entry.mapping_target
    if target is a virtual model:            // recursive, not terminal
      return resolve(target, group, stack, depth+1, priorityOverride)
    return [model-only {ChannelID: 0, Model: target, Priority: priorityOverride or modelRedirectDefaultPrio}]
  // redirect:
  out = []
  for t in entry.targets:                    // already priority-sorted (higher first)
    if t is nested ref (sentinel):
      out += resolve(t.Model, group, stack, depth+1, t.Priority)   // sentinel priority threads down
    else if t.ChannelID > 0:
      cand = {t.ChannelID, Model: t.Model or entry.Name, Priority: t.Priority}
      out += [cand]
  return out
```

- Depth limit (`modelRedirectMaxExpandDepth`) and a per-resolve visit `stack`
  shared across mapping + redirect traversal detect runaway/cycles at runtime.
- Redirect nodes reached via a mapping chain keep their **own internal
  priorities** (black-box, same as today's nested redirect); the
  `priorityOverride` threads only through mapping chains and lands on the final
  model-only candidate.
- Termination: mapping targets not in the virtual list → model-only terminal;
  redirect chains end at channel hops.
- Candidate cap (`modelRedirectMaxExpandedCandidates`) retained.
- `RedirectCandidate` gets `IsModelOnly() bool` (`ChannelID == 0 && Model != ""`).

### 5. Filtering / ordering (mostly unchanged)

- `FilterRedirectCandidates`: keep model-only candidates as-is (skip channel
  access/path checks — `GetRandomSatisfiedChannel` applies path filtering at pick
  time). Channel-bound candidates unchanged.
- `FilterRedirectCooldownDown`: **do not drop model-only candidates** (today it
  drops `ChannelID <= 0`). Cooldown for model-only hops is enforced at pick time
  (see §6).
- `OrderRedirectCandidates` / `OrderRedirectCandidatesWithAffinity`: unchanged
  (operate on priority runs; affinity matches `ChannelID`, so model-only hops
  never match a bound channel — documented limitation, see Edge cases).

### 6. Runtime selection + retry (slot walk)

New shared helper (middleware layer) resolves the **n-th attempt slot** from the
candidate list to `(channel, attemptModel)`:

- **channel-bound candidate** → one slot: `GetChannelForRedirect(id)`;
  `attemptModel = AttemptModel(clientModel, cand)`.
- **model-only candidate** → `RetryTimes + 1` slots (channel-priority tiers):
  slot level `k` uses `model.GetRandomSatisfiedChannel(effectiveGroup, cand.Model, k, path)`.
  When the probe returns nil (no channel at that tier), the walk advances to the
  next candidate with level 0. If the selected channel's `(channel, model)`
  cooldown is armed, the walk skips it and advances.

`tryModelRedirectSelection` (first pick = slot 0):
- channel-bound → existing path.
- model-only → probe tier 0; nil → advance to next candidate; all unavailable →
  existing "no available channel" abort.
- Store `ContextKeyModelRedirectCandidates` as the candidate list starting at the
  first usable candidate (earlier dropped hops excluded), plus
  `ContextKeyModelRedirectActive` / `ContextKeyModelRedirectClientModel` as today.
- **Store the resolved effective group** in a new context key
  `ContextKeyModelRedirectGroup` (in `constant/personal.go`). The retry slot walk
  needs it to select channels for model-only hops; for auto-group requests this
  is the group found by walking the token/user auto groups, for normal requests
  it is the request group.

`controller/relay.go getChannel` retry branch:
- Replace `idx := retry; cand := cands[idx]` with the slot walk above; model-only
  picks read the group from `ContextKeyModelRedirectGroup`.
- For model-only hops set `info.OriginModelName` / `info.UpstreamModelName =
  attemptModel` (the mapped model), `info.IsModelMapped = false` (so the
  channel's own `model_mapping` applies via `ModelMappedHelper`, exactly like a
  normal request for that model), recompute price via `helper.ModelPriceHelper`.
- `maxRetry = max(RetryTimes, len(cands)-1 + modelOnlyCount*RetryTimes)`:
  - `RetryTimes = 0` → one attempt per candidate (today's redirect semantics).
  - `RetryTimes > 0` → each model-only candidate gets the same channel-tier budget
    a normal request for that model would have.

### 7. Billing / plaza / logs

- `ModelRedirectDisplaySourceModel`: mapping entries return `MappingTarget`
  (billing source = mapped model's ratio/price).
- `ForEachEnabledModelRedirect`: include mapping entries;
  `displaySource = MappingTarget`.
- `IsModelRedirectVirtual` / `GetEnabledModelRedirectNamesForGroup`: work
  automatically once mapping entries are in the cache (plaza, `ListModels`,
  group model lists, `allowModelInUserList`).
- Log audit: existing `model_redirect` (original virtual name),
  `model_redirect_attempt` (mapped model), `model_redirect_retry_index` cover the
  mapping case without new fields.

### 8. Frontend (`web/src/features/models/`)

- `api-model-redirect.ts`: add `mode` and `mapping_target` to `ModelRedirect` and
  `ModelRedirectInput`.
- Table (`model-redirect-section.tsx`): new mode badge column
  (映射 / 重定向); the 目标 column renders `→ gpt-5.6` for mapping rows.
- Drawer: mode toggle (segmented: 模型映射 / 模型重定向) at the top.
  - mapping mode: single 「映射到」model-name input (suggestions from virtual
    model names + known model list where cheap); targets editor hidden.
  - redirect mode: existing targets editor; mapping input hidden.
  - Both local states preserved across mode switches; save submits only the
    active mode's data (backend keeps the other).
- Validation mirrors backend per mode.

## Error handling / edge cases

- **Mapped model has no channel in the group** → slot walk advances to the next
  candidate; all exhausted → existing "no available channel" error.
- **Mapping target equals another virtual name** → recursive resolve (redirect
  logic or further mapping), never treated as a terminal literal. If the
  referenced virtual is disabled/renamed **after save** (the cache only holds
  enabled entries), the target degrades to a literal model name and typically
  fails with "no available channel" — validation already forbade this at save
  time, so this is an operational edge, not a silent misroute.
- **Mapping cycle / self-reference** → rejected at save time (extended cycle
  detection); runtime stack as backstop.
- **`RetryTimes = 0`** → one attempt per candidate (redirect semantics preserved);
  mapping hops still fall back across the priority chain.
- **Cooldown** — model-only hop failure records `(channel, mapped model)`; pick
  time checks the selected channel's cooldown and advances.
- **Affinity** — a `channel_affinity_setting` rule matching a mapping virtual name
  cannot pin a channel (model-only hops carry no channel to promote). Documented
  limitation; affinity continues to work for channel-bound redirect hops.
- **Auto group** (`usingGroup == "auto"`) — the middleware already walks token/
  user auto groups to find the first group where the virtual model resolves; the
  model-only pick uses the resolved concrete `effectiveGroup`.

## Out of scope

- Weighted LB among mapping targets (stays `Weight`-reserved as today).
- Per-mapping-hop channel-affinity pinning.
- Editing/clearing the inactive mode's stored config via the UI (not needed —
  it is inert until the mode is switched back).

## Files to change

Backend:
- `constant/personal.go` — new `ContextKeyModelRedirectGroup`.
- `model/model_redirect.go` — `Mode`/`MappingTarget` fields, constants,
  `RedirectCandidate.IsModelOnly`, cache entry fields + build, recursive
  resolver, validation (mode/mapping_target/cycle edges), CRUD non-destructive
  update, display/plaza helpers.
- `middleware/model_redirect.go` — slot-walk helper + `tryModelRedirectSelection`
  first-pick (model-only support) + store effective group.
- `controller/relay.go` — `getChannel` slot walk + `maxRetry` formula.
- `model/model_redirect_cooldown.go` — keep model-only candidates in
  `FilterRedirectCooldownDown`.
- Tests: `model/model_redirect_test.go` (+ cooldown test if touched).

Frontend:
- `web/src/features/models/api-model-redirect.ts`
- `web/src/features/models/components/model-redirect-section.tsx`
- `web/src/features/models/index.tsx` (only if the mode toggle needs shared state
  in the provider; otherwise the drawer is self-contained)

## Testing

- `model/model_redirect_test.go` (style: deterministic table tests,
  `testify/require` for setup/fatal):
  - cache build: mapping entry with/without groups/target; redirect entry; both.
  - resolution: direct mapping → single model-only candidate; redirect → mapping
    nested (priority threading); mapping → mapping chain; mapping → redirect;
    non-virtual name → `ok=false`.
  - priority/filter: model-only candidates survive `FilterRedirectCandidates` and
    `FilterRedirectCooldownDown`; ordering across mixed candidates.
  - validation: empty mapping target, mapping target too long, mode invalid,
    mapping cycle rejected, self-reference rejected.
  - CRUD: update in mapping mode preserves targets; update in redirect mode
    preserves `mapping_target`.
- Slot-walk logic (middleware/relay): pure-function tests for slot → candidate
  resolution with mixed candidates and tier probing (light; no full relay stack).
- Frontend: drawer mode-switch state preservation and per-mode validation, if the
  project's frontend test setup permits.
