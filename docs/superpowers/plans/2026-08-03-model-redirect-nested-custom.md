# Nested Custom Model Redirect Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow model-redirect targets to pick a special channel “Custom redirect” (`channel_id = -1`) that references another virtual model by name, expanding at runtime so parent rules stay in sync when the child changes.

**Architecture:** Sentinel `channel_id = -1` stores a reference (child virtual name in `model`). Cache keeps sentinel candidates. `ResolveModelRedirect` recursively expands nested refs into a flat real-channel candidate list (black-box order, depth ≤ 32, cycle-safe). Save-time validation rejects self-ref and graph cycles. UI synthesizes the special channel option and switches the model picker to enabled virtual names.

**Tech Stack:** Go 1.22+, GORM, Gin, existing `model/model_redirect.go`, React 19 + TypeScript frontend (`model-redirect-section.tsx`), i18next.

**Spec:** `docs/superpowers/specs/2026-08-03-model-redirect-nested-custom-design.md`

## Global Constraints

- No DB schema migration; reuse `model_redirect_targets.channel_id` + `model`.
- Sentinel: `ModelRedirectSentinelChannelID = -1` (exact value).
- Reference semantics: never snapshot child targets at save time.
- Expand is black-box nested: full child chain before parent’s next target.
- Unlimited nesting with cycle detection; engineering depth cap 32; expanded candidate cap 128.
- UI virtual options: enabled redirects only; exclude self when editing.
- Personal low-conflict: expand/validate in `model/` package; middleware may stay unchanged if expand is inside `ResolveModelRedirect`.
- Backend tests: `github.com/stretchr/testify/require` + `assert` for new/rewritten tests where practical; existing tests may keep stdlib style for minimal churn.
- Frontend user-facing strings: English i18n keys via `t('...')`.

## File Structure

| File | Role |
|------|------|
| `constant/personal.go` | Export sentinel constant `-1` |
| `model/model_redirect.go` | Cache accept sentinel; expand inside Resolve; validate nested + cycles |
| `model/model_redirect_test.go` | Expand, cycle, validation, group gate tests |
| `middleware/model_redirect.go` | No change if Resolve expands (verify only) |
| `web/src/features/models/components/model-redirect-section.tsx` | Custom channel option, model source, draft mapping, display |
| `web/src/i18n/locales/*.json` | `Custom redirect` and related keys |
| `docs/个性化需求.md` | Document nested custom redirect |

---

### Task 1: Sentinel constant + cache accepts nested targets

**Files:**
- Modify: `constant/personal.go`
- Modify: `model/model_redirect.go` (`buildModelRedirectCacheMap`, optional helper on candidate)
- Test: `model/model_redirect_test.go`

**Interfaces:**
- Produces: `constant.ModelRedirectSentinelChannelID = -1`
- Produces: cache entries may contain `RedirectCandidate{ChannelID: -1, Model: "child-name", ...}`
- Produces: `func (c RedirectCandidate) IsNestedRedirect() bool` returning `c.ChannelID == constant.ModelRedirectSentinelChannelID`

- [ ] **Step 1: Write failing test that cache-shaped data can hold a sentinel target**

Append to `model/model_redirect_test.go`:

```go
func TestRedirectCandidate_IsNestedRedirect(t *testing.T) {
	require.True(t, RedirectCandidate{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "x"}.IsNestedRedirect())
	require.False(t, RedirectCandidate{ChannelID: 1, Model: "x"}.IsNestedRedirect())
}
```

Add imports: `github.com/stretchr/testify/require`, `github.com/QuantumNous/new-api/constant`.

- [ ] **Step 2: Run test — expect compile/fail**

Run: `go test ./model/ -run TestRedirectCandidate_IsNestedRedirect -count=1`

Expected: FAIL (undefined `IsNestedRedirect` and/or missing constant).

- [ ] **Step 3: Add constant + helper + fix cache builder**

In `constant/personal.go` inside the personal const block:

```go
// ModelRedirectSentinelChannelID marks a model-redirect target as a nested
// virtual-model reference. Not a channels row; target.model holds the child name.
ModelRedirectSentinelChannelID = -1
```

In `model/model_redirect.go`:

```go
func (c RedirectCandidate) IsNestedRedirect() bool {
	return c.ChannelID == constant.ModelRedirectSentinelChannelID
}
```

Change `buildModelRedirectCacheMap` target loop from “skip if `!t.Enabled || t.ChannelId <= 0`” to:

```go
for _, t := range targets {
	if !t.Enabled {
		continue
	}
	modelName := strings.TrimSpace(t.Model)
	if t.ChannelId == constant.ModelRedirectSentinelChannelID {
		if modelName == "" {
			continue
		}
		entry.Targets = append(entry.Targets, RedirectCandidate{
			ChannelID: constant.ModelRedirectSentinelChannelID,
			Model:     modelName,
			Priority:  t.Priority,
			Weight:    t.Weight,
		})
		continue
	}
	if t.ChannelId <= 0 {
		continue
	}
	entry.Targets = append(entry.Targets, RedirectCandidate{
		ChannelID: t.ChannelId,
		Model:     modelName,
		Priority:  t.Priority,
		Weight:    t.Weight,
	})
}
```

Add import `"github.com/QuantumNous/new-api/constant"` if missing.

- [ ] **Step 4: Re-run test**

Run: `go test ./model/ -run TestRedirectCandidate_IsNestedRedirect -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add constant/personal.go model/model_redirect.go model/model_redirect_test.go
git commit -m "feat(model-redirect): accept sentinel nested targets in cache"
```

---

### Task 2: Runtime expand inside ResolveModelRedirect

**Files:**
- Modify: `model/model_redirect.go` (`ResolveModelRedirect` + expand helpers)
- Test: `model/model_redirect_test.go`

**Interfaces:**
- Consumes: cache with possible sentinel targets (Task 1)
- Produces: `ResolveModelRedirect(clientModel, usingGroup)` returns **only real-channel** candidates (expanded), still `ok=false` when top-level name missing / group mismatch / expand yields empty
- Produces (unexported helpers):
  - `const modelRedirectMaxExpandDepth = 32`
  - `const modelRedirectMaxExpandedCandidates = 128`
  - `func expandModelRedirectTargets(name, usingGroup string, depth int, stack map[string]struct{}) []RedirectCandidate`

**Expand rules (must match spec):**
1. If `depth > 32` → log + return nil for that branch.
2. If `name` already in `stack` → log + return nil (cycle).
3. Look up cache entry for `name`; if nil or group not allowed → return nil.
4. For each target in entry order (already priority DESC in cache):
   - Nested: `append(expand(childName, group, depth+1, stack)...)`
   - Real: append as-is
5. Cap total length at 128; truncate + log if exceeded.
6. Top-level `ResolveModelRedirect`: if expand result empty → `ok=false` (no usable real hops).

- [ ] **Step 1: Write failing expand tests**

```go
func TestResolveModelRedirect_ExpandsNestedBlackBox(t *testing.T) {
	// Parent P: nested→child (P200), real ch=9 model=m9 (P100)
	// Child C: real ch=1 m1 (P50), real ch=2 m2 (P40)
	// Expected order for group default: ch1, ch2, ch9
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"child": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "m1", Priority: 50},
				{ChannelID: 2, Model: "m2", Priority: 40},
			},
		},
		"parent": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "child", Priority: 200},
				{ChannelID: 9, Model: "m9", Priority: 100},
			},
		},
	}
	modelRedirectLoaded = true
	modelRedirectCacheMu.Unlock()
	t.Cleanup(func() {
		modelRedirectCacheMu.Lock()
		modelRedirectCache = nil
		modelRedirectLoaded = false
		modelRedirectCacheMu.Unlock()
	})

	cands, ok := ResolveModelRedirect("parent", "default")
	require.True(t, ok)
	require.Len(t, cands, 3)
	assert.Equal(t, 1, cands[0].ChannelID)
	assert.Equal(t, "m1", cands[0].Model)
	assert.Equal(t, 2, cands[1].ChannelID)
	assert.Equal(t, 9, cands[2].ChannelID)
	for _, c := range cands {
		require.False(t, c.IsNestedRedirect(), "resolve must not return sentinels")
	}
}

func TestResolveModelRedirect_NestedChildGroupMismatchSkipsBranch(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"child": {
			Groups: map[string]struct{}{"vip": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "m1", Priority: 50},
			},
		},
		"parent": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "child", Priority: 200},
				{ChannelID: 9, Model: "m9", Priority: 100},
			},
		},
	}
	modelRedirectLoaded = true
	modelRedirectCacheMu.Unlock()
	t.Cleanup(func() {
		modelRedirectCacheMu.Lock()
		modelRedirectCache = nil
		modelRedirectLoaded = false
		modelRedirectCacheMu.Unlock()
	})

	cands, ok := ResolveModelRedirect("parent", "default")
	require.True(t, ok)
	require.Len(t, cands, 1)
	assert.Equal(t, 9, cands[0].ChannelID)
}

func TestResolveModelRedirect_RuntimeCycleDoesNotPanic(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"a": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "b", Priority: 1},
			},
		},
		"b": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "a", Priority: 1},
			},
		},
	}
	modelRedirectLoaded = true
	modelRedirectCacheMu.Unlock()
	t.Cleanup(func() {
		modelRedirectCacheMu.Lock()
		modelRedirectCache = nil
		modelRedirectLoaded = false
		modelRedirectCacheMu.Unlock()
	})

	_, ok := ResolveModelRedirect("a", "default")
	require.False(t, ok)
}
```

Add `assert` import from testify.

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test ./model/ -run 'TestResolveModelRedirect_Expands|TestResolveModelRedirect_Nested|TestResolveModelRedirect_Runtime' -count=1`

Expected: FAIL (still returns sentinels or wrong order).

- [ ] **Step 3: Implement expand and wire into Resolve**

Add constants near other modelRedirect max constants:

```go
modelRedirectMaxExpandDepth         = 32
modelRedirectMaxExpandedCandidates  = 128
```

Implement:

```go
func expandModelRedirectTargets(name, usingGroup string, depth int, stack map[string]struct{}) []RedirectCandidate {
	name = strings.TrimSpace(name)
	usingGroup = strings.TrimSpace(usingGroup)
	if name == "" || usingGroup == "" {
		return nil
	}
	if depth > modelRedirectMaxExpandDepth {
		common.SysLog(fmt.Sprintf("model redirect expand depth exceeded for %q", name))
		return nil
	}
	if _, seen := stack[name]; seen {
		common.SysLog(fmt.Sprintf("model redirect expand cycle at %q", name))
		return nil
	}
	// Caller holds no lock; take RLock for entry lookup only.
	modelRedirectCacheMu.RLock()
	entry := modelRedirectCache[name]
	var targets []RedirectCandidate
	if entry != nil && groupSetContains(entry.Groups, usingGroup) && len(entry.Targets) > 0 {
		targets = make([]RedirectCandidate, len(entry.Targets))
		copy(targets, entry.Targets)
	}
	modelRedirectCacheMu.RUnlock()
	if len(targets) == 0 {
		return nil
	}
	stack[name] = struct{}{}
	defer delete(stack, name)

	out := make([]RedirectCandidate, 0, len(targets))
	for _, t := range targets {
		if t.IsNestedRedirect() {
			child := strings.TrimSpace(t.Model)
			if child == "" {
				continue
			}
			nested := expandModelRedirectTargets(child, usingGroup, depth+1, stack)
			out = append(out, nested...)
		} else if t.ChannelID > 0 {
			out = append(out, t)
		}
		if len(out) >= modelRedirectMaxExpandedCandidates {
			common.SysLog(fmt.Sprintf("model redirect expand truncated at %d for %q", modelRedirectMaxExpandedCandidates, name))
			return out[:modelRedirectMaxExpandedCandidates]
		}
	}
	return out
}
```

Change `ResolveModelRedirect` body after group/cache hit to:

```go
// Copy raw targets under RLock (may include sentinels).
modelRedirectCacheMu.RLock()
entry := modelRedirectCache[clientModel]
if entry == nil || len(entry.Targets) == 0 || !groupSetContains(entry.Groups, usingGroup) {
	modelRedirectCacheMu.RUnlock()
	return nil, false
}
// Release lock before expand (expand re-locks for children).
modelRedirectCacheMu.RUnlock()

stack := map[string]struct{}{}
out := expandModelRedirectTargets(clientModel, usingGroup, 0, stack)
if len(out) == 0 {
	return nil, false
}
return out, true
```

Note: top-level expand starts with `depth=0` and pushes `clientModel` first so self-nested is handled; raw entry is re-read inside expand — acceptable.

**Important:** Existing `TestResolveModelRedirect_GroupGate` expects 2 real candidates and cache isolation. Expand of real-only entries must preserve order and defensive copy (still return new slice). Re-run full model_redirect tests after implement.

- [ ] **Step 4: Run expand + existing redirect tests**

Run: `go test ./model/ -run ModelRedirect -count=1`

Expected: all PASS. If `GroupGate` fails due to lock/copy changes, fix until green.

Also run: `go test ./model/ -count=1` (full package).

- [ ] **Step 5: Commit**

```bash
git add model/model_redirect.go model/model_redirect_test.go
git commit -m "feat(model-redirect): expand nested custom redirect at resolve"
```

---

### Task 3: Save-time validation (sentinel, self-ref, cycles)

**Files:**
- Modify: `model/model_redirect.go` (`validateModelRedirectInput`, Create/Update callers)
- Test: `model/model_redirect_test.go`

**Interfaces:**
- Change signature to:
  ```go
  func validateModelRedirectInput(in *ModelRedirectInput, isCreate bool, selfName string) error
  ```
  - Create: `selfName = ""` (or `strings.TrimSpace(in.Name)` for self-ref on create against own name in targets)
  - Update: `selfName = existing.Name` before rename, and also treat `in.Name` as the node identity for the graph
- Graph node name for the rule being saved: `strings.TrimSpace(in.Name)`
- Nested edge: target with `ChannelId == ModelRedirectSentinelChannelID` → edge to `strings.TrimSpace(target.Model)`
- Cycle: after substituting this node’s outbound nested edges with the input’s nested refs, DFS from node name; if revisit on current path → error `"model redirect cycle detected"`

Validation rules per target:
- `channel_id == -1`: model required; must exist as **enabled** `ModelRedirect` with that name; must not equal `in.Name` (self); do **not** call `GetChannelById`
- `channel_id > 0`: existing channel-exists check
- else: `"invalid channel_id"`

For enabled nested target existence without full cache:

```go
var n int64
err := DB.Model(&ModelRedirect{}).Where("name = ? AND enabled = ?", child, true).Count(&n).Error
```

Cycle graph builder (when `DB != nil`):
1. Load all redirects: `name, enabled` + preload enabled targets (or all targets).
2. Build `map[string][]string` of nested refs for every **enabled** redirect name.
3. Replace edges for `in.Name` with nested refs from `in.Targets` (only enabled targets in input).
4. If creating disabled parent, still check cycle on the would-be graph if enabled later — **simpler rule:** always run cycle on the name being saved using input edges + other rows’ stored nested edges (include disabled nodes’ edges only if you load all rows; prefer load all rows with targets for stable graph).

Recommended: load **all** `ModelRedirect` with `Preload("Targets")`, build edges only from targets where `Enabled && ChannelId == sentinel && Model != ""`.

- [ ] **Step 1: Write failing validation tests (pure unit with mocked cache/DB if heavy)**

Prefer table tests that call an unexported helper if DB setup is hard. Minimal approach using sqlite in-memory like other model tests, **or** extract:

```go
func detectModelRedirectCycle(node string, edges map[string][]string) bool
```

Test cycle helper without DB:

```go
func TestDetectModelRedirectCycle(t *testing.T) {
	edges := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	require.True(t, detectModelRedirectCycle("a", edges))
	require.False(t, detectModelRedirectCycle("a", map[string][]string{"a": {"b"}, "b": {}}))
}
```

And a validation unit that does not need channels for sentinel-only — may need DB for “child exists”. Use in-memory sqlite:

```go
func setupModelRedirectTestDB(t *testing.T) {
	// same pattern as controller tests: gorm sqlite memory, model.DB = db, AutoMigrate ModelRedirect + Target
}
```

Then:

```go
func TestValidateModelRedirectInput_NestedRules(t *testing.T) {
	setupModelRedirectTestDB(t)
	// insert enabled child "deepseek-flash" with a dummy real target (channel can be skipped if we only Count by name)
	// ...
	err := validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "deepseek-flash", Priority: 10},
		},
	}, true, "")
	require.NoError(t, err)

	err = validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "parent", Priority: 10},
		},
	}, true, "")
	require.Error(t, err)
}
```

For real-channel targets in the same suite, insert a `Channel` row or skip mixed cases.

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./model/ -run 'TestDetectModelRedirectCycle|TestValidateModelRedirectInput_Nested' -count=1`

- [ ] **Step 3: Implement helpers + update validate + Create/Update**

```go
func detectModelRedirectCycle(start string, edges map[string][]string) bool {
	// DFS with path set
}

func buildModelRedirectNestedEdgeMap(rows []*ModelRedirect) map[string][]string { ... }

func validateModelRedirectInput(in *ModelRedirectInput, isCreate bool, selfName string) error {
	// existing name/groups/targets length checks...
	// per target channel rules as above
	// then cycle:
	// edges := load from DB + override in.Name
	// if detectModelRedirectCycle(in.Name, edges) { return fmt.Errorf("model redirect cycle detected") }
}
```

Update callers:
- `CreateModelRedirect`: `validateModelRedirectInput(in, true, "")`
- `UpdateModelRedirect`: after loading existing, `validateModelRedirectInput(in, false, existing.Name)`

- [ ] **Step 4: Run tests**

Run: `go test ./model/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add model/model_redirect.go model/model_redirect_test.go
git commit -m "feat(model-redirect): validate nested refs and reject cycles"
```

---

### Task 4: Frontend — Custom redirect channel + model picker

**Files:**
- Modify: `web/src/features/models/components/model-redirect-section.tsx`
- Modify: `web/src/i18n/locales/en.json` (and run/sync other locales or add keys to zh/zh-TW/fr/ru/ja/vi as project expects)
- Optionally export constant: `web/src/features/models/api-model-redirect.ts`  
  `export const MODEL_REDIRECT_SENTINEL_CHANNEL_ID = -1`

**Interfaces:**
- Consumes: `listModelRedirects()` data already loaded in section
- Produces: payload targets with `channel_id: -1` and `model: childVirtualName`

- [ ] **Step 1: Constants + draft mapping**

```ts
export const MODEL_REDIRECT_SENTINEL_CHANNEL_ID = -1 // in api-model-redirect.ts or local const
```

Update `draftsToInputTargets`:

```ts
if (priority <= 0) continue
const isNested = draft.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID
if (!isNested && draft.channel_id <= 0) continue
if (isNested && !draft.model.trim()) continue
// allow channel_id -1 through
```

Update `channelDisplayName`:

```ts
if (channelId === MODEL_REDIRECT_SENTINEL_CHANNEL_ID) return t('Custom redirect')
// note: channelDisplayName currently has no t — either pass label or handle at call sites
```

Prefer: at display sites, if `target.channel_id === -1`, render `t('Custom redirect') → {model}`.

- [ ] **Step 2: Channel options include sentinel first**

In the edit sheet:

```ts
const channelOptions = useMemo(() => {
  const custom: ChannelOption = {
    id: MODEL_REDIRECT_SENTINEL_CHANNEL_ID,
    name: t('Custom redirect'),
    models: '',
  }
  return [custom, ...sortChannelsByName(channelsRaw)]
}, [channelsRaw, t])
```

Pass `channelOptions` into cards instead of raw channels.

- [ ] **Step 3: Virtual model options for nested rows**

In sheet, from `listModelRedirects` query (table already loads list — pass `virtualNames: string[]` into sheet or useQuery again):

```ts
const nestedModelOptions = useMemo(() => {
  return (allRedirects || [])
    .filter((r) => r.enabled && r.name !== name.trim())
    .map((r) => r.name)
    .sort((a, b) => a.localeCompare(b))
}, [allRedirects, name])
```

`RedirectTargetCard` props:
- `nestedModelOptions: string[]`
- When `target.channel_id === SENTINEL`, model Select uses `nestedModelOptions` only, **no passthrough**, required label `t('Redirect model')`, disabled if options empty.

`onChannelChange`: if new id is sentinel, clear model; if switching from sentinel to real, clear model.

Select value for channel must accept `-1`:

```ts
value={
  target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID || target.channel_id > 0
    ? String(target.channel_id)
    : undefined
}
```

- [ ] **Step 4: Client-side save validation**

Replace:

```ts
targets.some((target) => !target.channel_id || target.channel_id <= 0)
```

with:

```ts
targets.some((target) => {
  if (target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID) {
    return !target.model.trim()
  }
  return !target.channel_id || target.channel_id <= 0
})
```

Error messages via `t(...)` where already using Chinese hardcode — match file style (file currently uses Chinese literals; keep consistency with surrounding code unless i18n already used in this component).

- [ ] **Step 5: Table column display**

In targets column renderer:

```tsx
if (target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID) {
  label = `${t('Custom redirect')} → ${target.model || '?'}`
} else {
  // existing channel name + model
}
```

- [ ] **Step 6: i18n keys**

Add to `web/src/i18n/locales/en.json` (and other locales):

```json
"Custom redirect": "Custom redirect",
"Redirect model": "Redirect model",
"Select a redirect model": "Select a redirect model"
```

Chinese example in `zh.json`: `"Custom redirect": "自定义重定向"`, etc.

If the component does not use `useTranslation` yet, add `const { t } = useTranslation()` and migrate only the new strings (or the custom-redirect labels).

- [ ] **Step 7: Manual sanity / typecheck**

Run from `web/`:

```bash
bun run build
```

Expected: success (or at least tsc clean for this file). Fix any type errors.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/models/components/model-redirect-section.tsx web/src/features/models/api-model-redirect.ts web/src/i18n/locales/*.json
git commit -m "feat(web): custom redirect channel in model-redirect editor"
```

---

### Task 5: Docs + smoke verification

**Files:**
- Modify: `docs/个性化需求.md` (section 三、模型重定向)

- [ ] **Step 1: Document nested custom redirect**

Add under runtime / admin:

```markdown
#### 自定义重定向（嵌套）

- 目标渠道可选哨兵「自定义重定向」`channel_id = -1`，`model` = 其它虚拟模型名。
- 引用语义：运行时展开子规则当前目标链；改子规则无需改父规则。
- 保存时校验自指与环；运行时 depth≤32、展开上限 128。
```

Update file inventory table with sentinel constant file if needed.

- [ ] **Step 2: Backend smoke**

```bash
go test ./model/ ./middleware/ ./controller/ -count=1 -timeout 120s
go build -o /dev/null .
```

Expected: PASS / BUILD_OK.

- [ ] **Step 3: Commit**

```bash
git add docs/个性化需求.md
git commit -m "docs: model-redirect nested custom redirect"
```

---

## Spec Coverage Checklist

| Spec requirement | Task |
|------------------|------|
| Sentinel `-1` | Task 1 |
| Cache keeps nested targets | Task 1 |
| Runtime black-box expand | Task 2 |
| Depth 32 / cycle runtime | Task 2 |
| Expand cap 128 | Task 2 |
| Group gate on child | Task 2 |
| Validate sentinel + enabled child | Task 3 |
| Validate self-ref + cycle | Task 3 |
| Allow delete of referenced child | no code (default) |
| UI custom channel | Task 4 |
| UI model = other virtual names | Task 4 |
| draftsToTargets allows -1 | Task 4 |
| List display | Task 4 |
| i18n | Task 4 |
| 个性化需求.md | Task 5 |
| Middleware thin / Resolve expands | Task 2 (no middleware change) |

## Plan Self-Review

1. **Spec coverage:** All acceptance criteria map to tasks 1–5.
2. **Placeholders:** None intentional; test snippets are concrete.
3. **Types:** `ModelRedirectSentinelChannelID` / `IsNestedRedirect` / `validateModelRedirectInput(..., selfName)` consistent across tasks.
4. **Risk:** `ResolveModelRedirect` behavior change is covered by expanding only when nested targets exist; leaf tests stay green.
