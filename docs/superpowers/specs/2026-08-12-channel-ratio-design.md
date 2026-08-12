# 渠道倍率（Channel Ratio）Design

**Date:** 2026-08-12
**Status:** Approved for spec (brainstorming)
**Scope:** Per-channel billing multiplier applied on top of group ratio
**Related:** `model.Channel`, `types.GroupRatioInfo`, `relay/helper/price.go`, `service/text_quota.go`, `pkg/billingexpr`, `service/task_billing.go`, channel mutate drawer

## 1. Problem

Today a channel is priced as: **group ratio × model price**. Operators who want a
specific channel to be cheaper (or more expensive) at the upstream level must put
that channel into its own group and set the group's ratio — which forces clients to
switch API keys/groups. There is no per-channel price adjustment.

## 2. Goals and non-goals

### Goals

- Add a per-channel billing multiplier `channel ratio`（渠道倍率）, default `1` (no adjustment).
- New settlement formula: **group ratio × channel ratio × model price + other fees**.
  - Channel ratio multiplies only the model-price block:
    `(base tokens × model ratio | fixed model price) × group ratio × channel ratio × other ratios`.
  - Other fees (audio input price, tool-call surcharges) are **not** scaled by channel ratio.
- Channel ratio applies consistently to pre-consume, settlement, per-call billing,
  tiered-expr billing, async task settlement, and channel tests.
- Allow `0 ≤ ratio ≤ 100`; `0` means the channel is free (consistent with group-ratio
  semantics where `0` = free group).

### Non-goals

- No channel-ratio column in the channel list UI (keep the list unchanged).
- No ratio sync across instances (`SyncableChannel` only carries identity fields).
- No change to group-ratio semantics or to the "other fees" pricing itself.

## 3. Data model and validation

- `model.Channel` gains `Ratio *float64` (`json:"ratio" gorm:"column:ratio;default:1"`)
  with getter `GetRatio() float64` returning `1.0` when nil. `&Channel{}` is already in
  `AutoMigrate`, so the column is created on all three supported databases.
- `validateChannel`: when `Ratio != nil`, require `0 <= ratio <= 100` and reject NaN/+Inf.
  Nil is treated as 1.0 (no adjustment).

## 4. Runtime flow

Mirrors the existing group-ratio flow with a parallel channel-ratio chain:

1. New context key `constant.ContextKeyChannelRatio`.
2. `middleware.SetupContextForSelectedChannel` writes `channel.GetRatio()` into the context.
3. `types.GroupRatioInfo` gains `ChannelRatio float64` (default `1.0`).
4. `relay/helper.HandleGroupRatio` reads the context key and populates
   `GroupRatioInfo.ChannelRatio` (default `1.0` when absent).
5. `controller/relay.go getChannel`: move the `HandleGroupRatio` refresh to **after**
   `SetupContextForSelectedChannel`, so retries to a different channel capture that
   channel's ratio. Safe reorder: the `auto_group` context key is already set by
   `CacheGetRandomSatisfiedChannel` before this point, and `SetupContextForSelectedChannel`
   does not touch it.

Because pre-consume runs after the middleware already selected the first channel, the
first attempt's pre-consume uses the real channel ratio. Retries that switch channels
settle the delta via the existing `BillingSession.Settle` path.

## 5. Price computation (pre-consume)

- `ModelPriceHelper` non-price branch: `ratio = modelRatio × GroupRatio × ChannelRatio`
  (quota = estimated tokens × ratio, via `common.QuotaFromFloatStrict`).
- Price branch: `ApplyOtherRatiosToFloat(modelPrice × QuotaPerUnit × GroupRatio × ChannelRatio)`.
- `ModelPriceHelperPerCall`: both the `modelPrice` and `modelRatio/2` branches multiply
  `ChannelRatio`.
- Free-model logic: treat `GroupRatio == 0 || ChannelRatio == 0` as free →
  `preConsumedQuota = 0`, `freeModel = true` (existing `EnableFreeModelPreConsume` gate preserved).
- `modelPriceHelperTiered`: `BillingSnapshot` gains `ChannelRatio float64`;
  `EstimatedQuotaAfterGroup = QuotaRoundStrict(quotaBeforeGroup × GroupRatio × ChannelRatio)`.

## 6. Settlement

- `service.calculateTextQuotaSummary`:
  - Ratio branch: `ratio = dModelRatio × dGroupRatio × dChannelRatio`.
  - Price branch: `dModelPrice × dQuotaPerUnit × dGroupRatio × dChannelRatio`.
  - `audioInputQuota` and `ToolCallSurchargeQuota` stay group-ratio-scaled only
    (no channel ratio), per the approved formula.
  - The "minimum 1 quota" guard (`!ratio.IsZero()`) naturally covers channel ratio 0.
- `pkg/billingexpr`:
  - `ComputeTieredQuotaWithRequest`: `afterGroup = QuotaRoundChecked(quotaBeforeGroup × snap.GroupRatio × snap.ChannelRatio)`.
  - `service.refreshTieredBillingGroup`: also refresh `snap.ChannelRatio` and include it
    in `EstimatedQuotaAfterGroup`.
  - `composeTieredTextQuota`: tool-surcharge term keeps `snap.GroupRatio` only.
- Async task billing:
  - `model.TaskBillingContext` gains `ChannelRatio float64` (snapshot at submission in
    `controller/relay.go` from `relayInfo.PriceData.GroupRatioInfo.ChannelRatio`).
  - `RecalculateTaskQuotaByTokens` multiplies the channel ratio:
    `totalTokens × modelRatio × finalGroupRatio × channelRatio × otherMultiplier`.
- Logs: consume log `other` gains `channel_ratio`
  (`GenerateTextOtherInfo` / `GenerateClaudeOtherInfo` and WSS/audio wrappers gain a
  `channelRatio` param; `LogTaskConsumption` and `taskBillingOther` include it;
  `PriceData.ToSetting()` includes it).

## 7. Frontend

- Channel mutate drawer "基础设置" section: new "渠道倍率" number input
  (default 1, min 0, max 100, step 0.01, hint: 0 = free channel, 1 = no adjustment).
- `web/src/features/channels/lib/channel-form.ts`: schema field with bounds validation,
  default value `1`, payload mapping (`ratio` in `toChannelPayload`, parse with default 1
  in `fromChannelPayload`).
- `channel-form-errors.ts`: error message for out-of-range ratio.
- i18n: en + zh keys (other locales fall back to en via existing tooling).
- Channel list table unchanged.

## 8. Channel test

`controller/channel-test.go` already calls `SetupContextForSelectedChannel` before
`ModelPriceHelper`, so the ratio flows automatically. Its log call gains the new
`channel_ratio` param.

## 9. Tests

- Backend:
  - `relay/helper/price_test.go`: pre-consume includes channel ratio; ratio 0 → free.
  - `service/text_quota_test.go`: settlement multiplies channel ratio on the base block;
    audio input / tool surcharge are not scaled.
  - `pkg/billingexpr/billingexpr_test.go` / `service/tiered_settle_test.go`:
    `ChannelRatio` in snapshot pre-consume and settle.
  - `controller/channel_test_internal_test.go` / channel validation: bounds 0..100,
    NaN/Inf rejected, nil → 1.
  - `service/task_billing_test.go`: task re-settlement applies channel ratio.
- Frontend: form validation for bounds (0..100), default 1.
