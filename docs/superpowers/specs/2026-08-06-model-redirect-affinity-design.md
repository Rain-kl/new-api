# Model Redirect Same-Priority Channel Affinity

Date: 2026-08-06
Status: Approved design (pending spec review)

## Goal

For a personal virtual model (`/models/redirect`), when the admin configures
multiple targets with the **same priority**, requests that share an affinity key
should prefer the **same channel** instead of the current per-request equal-share
shuffle. The feature must reuse new-api's existing channel-affinity machinery
end-to-end (rules, cache, recording, admin logs) — no parallel affinity system.

## Decisions (confirmed with requester)

1. **Reuse the existing `channel_affinity_setting` rule system.** The admin
   configures a rule whose `model_regex` matches the **virtual model name** and
   whose `key_sources` picks the session identity (e.g. `session_id`,
   `conversation_id`, `prompt_cache_key`, `metadata.user_id`). No new config
   concept is introduced.
2. **Affinity model dimension = virtual model name** (the client model). All
   attempt models under one redirect share a single binding, so a session stays
   on the same channel across all targets of that redirect.
3. **Failure semantics: keep the binding and preserve redirect HA.** If the
   preferred channel fails, the request keeps walking the same-priority pool and
   then lower priorities as today. The matched rule's `skip_retry_on_failure`
   is **ignored** for redirect requests so `ShouldSkipRetryAfterChannelAffinityFailure`
   cannot kill pool retries.

## Architecture / data flow

### 1. Selection (`middleware/model_redirect.go` → `tryModelRedirectSelection`)

After `FilterRedirectCandidates` + `FilterRedirectCooldownDown`:

1. Call `service.GetPreferredChannelByAffinity(c, clientModel, effectiveGroup)`.
   - Reads the cached preferred channel (if any) for the request's affinity key.
   - **Also sets the affinity context** (cache key / TTL / meta) so the existing
     `RecordChannelAffinity` call in the distributor persists the binding on success.
2. Call the new exported `service.ClearChannelAffinitySkipRetry(c)`.
   - Sets the skip-retry context flag to `false`, so `shouldRetry` in
     `controller/relay.go` keeps walking the pool even when the matched rule has
     `SkipRetryOnFailure: true`.
3. If the preferred channel is actually promoted, call
   `service.MarkChannelAffinityUsed(c, effectiveGroup, preferredID)` for admin log info.
4. Order: `filtered = model.OrderRedirectCandidatesWithAffinity(filtered, preferredID)`.
5. Pick `filtered[0]` as today.

### 2. Persistence (unchanged code path)

The distributor already calls `service.RecordChannelAffinity(c, channel.Id)` on
success (`c.Writer.Status() < 400`) for every request, including redirects. Since
the affinity context is now set during selection, the binding is written
automatically. `SwitchOnSuccess` (default true) records the actually used channel.

### 3. Retry / failure (unchanged)

`controller/relay.go` is untouched: retries walk the pre-ordered candidate list
(same-priority pool first, then lower priorities); hop cooldown
(`model/model_redirect_cooldown.go`) is unchanged; the affinity binding is kept
on failure.

### 4. New pure ordering helper (`model/model_redirect.go`)

```go
// OrderRedirectCandidatesWithAffinity promotes the bound channel to the front of
// each contiguous equal-priority run that contains it; runs without it keep the
// existing equal-share shuffle. preferredChannelID <= 0 behaves exactly like
// OrderRedirectCandidates.
func OrderRedirectCandidatesWithAffinity(cands []RedirectCandidate, preferredChannelID int) []RedirectCandidate
```

- Operates on **contiguous equal-priority runs only**; never regroups or
  re-sorts globally (nested-expand black-box order is preserved, same as today).
- Stable promotion: the preferred channel moves to the front of its run, other
  members keep relative order (no re-shuffle of the promoted run).
- `OrderRedirectCandidates` remains the `preferredChannelID = 0` path (existing
  tests/behavior untouched).

## Error handling / edge cases

- **No rule matches** (no affinity context): behavior is identical to today
  (equal-share shuffle).
- **Preferred channel cooled down** (`FilterRedirectCooldownDown` dropped it):
  not promoted; binding is retained.
- **Preferred channel disabled**: absent from the filtered pool; determine
  "disabled" by loading the channel (`model.CacheGetChannel`) and checking
  `Status != enabled` (cooldown-only exclusions keep the channel enabled and do
  not clear the binding). When the channel is disabled and
  `ShouldKeepChannelAffinityOnChannelDisabled()` is false, reuse
  `service.ClearCurrentChannelAffinityCache(c)` to drop the stale binding
  (mirrors the existing distributor behavior).
- **Preferred channel at a different priority**: ignored; only same-priority-run
  promotion is applied, so priority semantics are never overridden.
- **Affinity cache read error**: `GetPreferredChannelByAffinity` already returns
  `(0, false)` on cache errors; selection falls back to today's shuffle.

## Files to change

- `model/model_redirect.go` — add `OrderRedirectCandidatesWithAffinity` (+ small
  run-index helper).
- `model/model_redirect_test.go` — unit tests for the new ordering.
- `middleware/model_redirect.go` — wire affinity read / skip-retry clear /
  mark-used / affinity-aware ordering into `tryModelRedirectSelection`.
- `service/channel_affinity.go` — add the tiny exported
  `ClearChannelAffinitySkipRetry(c *gin.Context)` helper.
- No DB migration, no frontend changes (config flows through the existing
  `channel_affinity_setting` settings).

## Testing

- `model/model_redirect_test.go`:
  - preferred channel in a same-priority run → promoted to front, stable.
  - preferred channel absent → equal-share shuffle across the run (today's
    behavior, existing `TestOrderRedirectCandidates_SamePriorityLoadBalance` keeps passing).
  - preferred channel in a higher/lower priority run → only its own run affected,
    priority order preserved.
  - single-element run with preferred → unchanged.
  - nested-expand black-box order preserved when preferred is in a
    non-contiguous position.
  - `preferredChannelID <= 0` → identical to `OrderRedirectCandidates`.
- Service / middleware:
  - `ClearChannelAffinitySkipRetry` makes `ShouldSkipRetryAfterChannelAffinityFailure`
    return `false` even when the rule has `SkipRetryOnFailure: true`.
  - `tryModelRedirectSelection` sets the affinity context (cache key present) and
    clears skip-retry on the redirect path. Keep these tests light; no brittle
    mocks of the whole relay stack.
