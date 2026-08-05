# IP / Proxy Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add admin IP/proxy pool management aligned with sub2api ops, and wire channels to select pool proxies (or custom URL) including batch assign.

**Architecture:** New `proxies` table holds managed proxies. Channel `setting` stores optional `proxy_id` plus resolved/custom `proxy` URL string so runtime keeps using `GetHttpClientWithProxy*`. Probe/quality/import/expiry logic is ported from `/Users/ryan/Code/Go/sub2api`. Frontend follows redemption-codes patterns under `web/src/features/proxies/`.

**Tech Stack:** Go 1.22+, Gin, GORM, testify; React 19, TypeScript, TanStack Query/Table/Router, react-hook-form, zod, i18next; Bun for frontend scripts.

**Spec:** `docs/superpowers/specs/2026-08-05-ip-proxy-management-design.md`

## Global Constraints

- JSON marshal/unmarshal only via `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr` (never raw `encoding/json` in business code).
- DB must work on SQLite, MySQL, PostgreSQL; prefer GORM; register new models with `RegisterMainDBModel`.
- Do not use GORM `default:true` boolean tags for business defaults.
- Port probe/quality/import/expiry algorithms from sub2api; adapt to new-api packages.
- Runtime outbound path continues to use `ChannelSettings.Proxy` string.
- UI reuses new-api components (data-table, Sheet, Dialog, Select/Combobox); do not port sub2api Vue styles.
- Backend tests use `github.com/stretchr/testify/require` and `assert`.
- Protected project names (new-api / QuantumNous branding) must not be removed or renamed.
- relaykit module must stay independently buildable (`cd relaykit && GOWORK=off go build ./...` if touching `relaykit/`).

## File Structure

| Path | Responsibility |
|------|----------------|
| `model/proxy.go` | Proxy entity, CRUD, list, bind/sync, expiry data ops |
| `model/proxy_test.go` | Unit/integration tests for model |
| `service/proxy_probe.go` | Connectivity probe + quality check (ported) |
| `service/proxy_probe_test.go` | Score/grade pure tests + probe with httptest |
| `controller/proxy.go` | Admin HTTP handlers |
| `controller/proxy_test.go` | Handler tests with sqlite test DB where needed |
| `router/proxy-router.go` | Register `/api/proxy` routes |
| `router/api-router.go` | Call `registerProxyRoutes` |
| `router/channel-router.go` | Add batch proxy route |
| `relaykit/dto/channel_settings.go` | Add `ProxyId` |
| `controller/channel.go` | Normalize settings; `BatchSetChannelProxy` |
| `main.go` | Start expiry sweep ticker |
| `web/src/features/proxies/*` | Admin IP management feature |
| `web/src/routes/_authenticated/proxies/index.tsx` | Route |
| `web/src/features/channels/*` | Form + bulk proxy UI |
| `web/src/hooks/use-sidebar-data.ts` | Nav item |
| `web/src/hooks/use-sidebar-config.ts` | Module toggle map |
| `web/src/i18n/locales/*.json` | Strings |

**Reference sources (read, then port):**

- `/Users/ryan/Code/Go/sub2api/backend/internal/service/proxy.go` — URL()
- `/Users/ryan/Code/Go/sub2api/backend/internal/service/admin_proxy.go` — test/quality/finalize
- `/Users/ryan/Code/Go/sub2api/backend/internal/service/admin_service.go` — quality targets
- `/Users/ryan/Code/Go/sub2api/backend/internal/repository/proxy_probe_service.go` — ProbeProxy
- `/Users/ryan/Code/Go/sub2api/backend/internal/handler/admin/proxy_data.go` — import/export
- `/Users/ryan/Code/Go/sub2api/backend/internal/service/proxy_fallback.go` — fallback walk
- `/Users/ryan/Code/Go/new-api/web/src/features/redemption-codes/*` — FE CRUD template
- `/Users/ryan/Code/Go/new-api/common/proxy_url.go` — strict URL validation

---

### Task 1: Proxy model — URL() and Validate()

**Files:**
- Create: `model/proxy.go`
- Create: `model/proxy_test.go`

**Interfaces:**
- Produces: `type Proxy struct`, constants `ProxyStatus*`, `ProxyFallback*`, `func (p *Proxy) URL() string`, `func (p *Proxy) Validate() error`, `func (p *Proxy) IsExpired(now int64) bool`, `func init() { RegisterMainDBModel(&Proxy{}) }`

- [ ] **Step 1: Write failing tests**

```go
package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyURL_NoAuth(t *testing.T) {
	p := &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
	assert.Equal(t, "http://proxy.example:8080", p.URL())
}

func TestProxyURL_WithAuth(t *testing.T) {
	p := &Proxy{Protocol: "socks5", Host: "1.2.3.4", Port: 1080, Username: "u", Password: "p"}
	assert.Equal(t, "socks5://u:p@1.2.3.4:1080", p.URL())
}

func TestProxyURL_UsernameOnlyNoAuth(t *testing.T) {
	p := &Proxy{Protocol: "http", Host: "h", Port: 1, Username: "u"}
	assert.Equal(t, "http://h:1", p.URL())
}

func TestProxyValidate_RequiresBackupWhenFallbackProxy(t *testing.T) {
	p := &Proxy{Name: "a", Protocol: "http", Host: "h", Port: 8080, FallbackMode: ProxyFallbackProxy}
	require.Error(t, p.Validate())
}

func TestProxyValidate_BackupCannotBeSelf(t *testing.T) {
	p := &Proxy{Id: 3, Name: "a", Protocol: "http", Host: "h", Port: 8080, FallbackMode: ProxyFallbackProxy, BackupProxyId: 3}
	require.Error(t, p.Validate())
}

func TestProxyValidate_OK(t *testing.T) {
	p := &Proxy{Name: "a", Protocol: "https", Host: "h", Port: 443, FallbackMode: ProxyFallbackNone}
	require.NoError(t, p.Validate())
	assert.Equal(t, ProxyStatusActive, p.Status)
	assert.Equal(t, 7, p.ExpiryWarnDays)
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./model/ -run 'TestProxy' -count=1
```

Expected: FAIL (undefined Proxy)

- [ ] **Step 3: Implement model skeleton**

Create `model/proxy.go` with:

- Constants for status/fallback
- Struct fields per design §4.1 (all probe/quality columns)
- `init()` → `RegisterMainDBModel(&Proxy{})`
- `URL()` using `net.JoinHostPort` + `url.UserPassword` only when both user+pass set
- `Validate()`: trim fields, protocol whitelist, port range, status default active, fallback rules, `expiry_warn_days` default 7 when 0, `common.ParseProxyURLStrict(p.URL())`
- `IsExpired(now int64)`: `expires_at > 0 && expires_at < now`
- `EffectiveStatus()`: expired if status expired or IsExpired

Do not implement CRUD yet beyond types needed for tests.

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test ./model/ -run 'TestProxy' -count=1
```

- [ ] **Step 5: Commit**

```bash
git add model/proxy.go model/proxy_test.go
git commit -m "feat(proxy): add Proxy model URL and validation"
```

---

### Task 2: Proxy persistence — Insert/Update/Get/List/Delete

**Files:**
- Modify: `model/proxy.go`
- Modify: `model/proxy_test.go`

**Interfaces:**
- Produces: `Insert`, `Update`, `UpdateProbeResult`, `GetProxyById`, `DeleteProxyById`, `ListProxies(start,num,filters)`, `GetAllActiveProxies`, `CheckProxyExists`, `ProxyListFilters`

- [ ] **Step 1: Write failing persistence tests** using in-memory SQLite pattern from existing model tests.

Look up how other model tests set `model.DB` (e.g. `controller` helpers or `model` package tests). Prefer a small helper in `proxy_test.go`:

```go
func setupProxyTestDB(t *testing.T) {
	t.Helper()
	// Open sqlite :memory:, assign model.DB, AutoMigrate(&Proxy{})
}
```

```go
func TestProxyInsertAndGet(t *testing.T) {
	setupProxyTestDB(t)
	p := &Proxy{Name: "n1", Protocol: "http", Host: "h", Port: 8080}
	require.NoError(t, p.Insert())
	got, err := GetProxyById(p.Id)
	require.NoError(t, err)
	assert.Equal(t, "n1", got.Name)
	assert.Equal(t, ProxyStatusActive, got.Status)
}

func TestProxyListFilterProtocol(t *testing.T) {
	setupProxyTestDB(t)
	require.NoError(t, (&Proxy{Name: "a", Protocol: "http", Host: "h", Port: 1}).Insert())
	require.NoError(t, (&Proxy{Name: "b", Protocol: "socks5", Host: "h", Port: 2}).Insert())
	list, total, err := ListProxies(0, 10, ProxyListFilters{Protocol: "socks5"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "b", list[0].Name)
}

func TestCheckProxyExists(t *testing.T) {
	setupProxyTestDB(t)
	require.NoError(t, (&Proxy{Name: "a", Protocol: "http", Host: "h", Port: 9, Username: "u", Password: "p"}).Insert())
	ok, err := CheckProxyExists("h", 9, "u", "p")
	require.NoError(t, err)
	assert.True(t, ok)
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./model/ -run 'TestProxyInsert|TestProxyList|TestCheckProxy' -count=1
```

- [ ] **Step 3: Implement persistence**

- `Insert`: Validate, timestamps via `common.GetTimestamp()`, `DB.Create`
- `Update`: Validate, `Select` all mutable columns including probe fields
- `UpdateProbeResult`: only probe/quality columns + updated_time
- `GetProxyById` / `DeleteProxyById` soft-delete via GORM
- `ListProxies`: filters protocol/status/search (id exact or LIKE name/host/username); status `active` excludes past expires_at; `expired` = status expired OR past expires_at; order whitelist; attach `channel_count` stub 0 for now if Task 3 not done — **prefer leave attach call to Task 3**
- `GetAllActiveProxies`: status=active order id desc
- `CheckProxyExists`: host+port+username+password exact

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./model/ -run 'TestProxy' -count=1
```

- [ ] **Step 5: Commit**

```bash
git add model/proxy.go model/proxy_test.go
git commit -m "feat(proxy): persist proxies with list filters"
```

---

### Task 3: Channel bind count, sync URL, set/clear

**Files:**
- Modify: `model/proxy.go`
- Modify: `model/proxy_test.go`
- Modify: `relaykit/dto/channel_settings.go` — add `ProxyId int \`json:"proxy_id,omitempty"\``

**Interfaces:**
- Produces: `CountChannelsByProxyID`, `ListChannelsByProxyID`, `ApplyProxyURLToChannels`, `ClearProxyFromChannels`, `SetChannelsProxy`, `ChannelSummaryForProxy`
- Consumes: `Channel.GetSetting` / `SetSetting`, `dto.ChannelSettings.ProxyId`

- [ ] **Step 1: Add ProxyId to ChannelSettings**

In `relaykit/dto/channel_settings.go` after `Proxy`:

```go
ProxyId int `json:"proxy_id,omitempty"`
```

- [ ] **Step 2: Write failing bind tests**

```go
func TestSetChannelsProxyAndCount(t *testing.T) {
	setupProxyTestDB(t)
	// also AutoMigrate Channel
	p := &Proxy{Name: "p", Protocol: "http", Host: "h", Port: 8080}
	require.NoError(t, p.Insert())
	ch := &Channel{Name: "c", Key: "k", Status: 1}
	require.NoError(t, DB.Create(ch).Error)

	n, err := SetChannelsProxy([]int{ch.Id}, p.Id, p.URL())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	count, err := CountChannelsByProxyID(p.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, ch.Id).Error)
	s := reloaded.GetSetting()
	assert.Equal(t, p.Id, s.ProxyId)
	assert.Equal(t, p.URL(), s.Proxy)
}

func TestApplyProxyURLToChannels(t *testing.T) {
	setupProxyTestDB(t)
	// create proxy + channel bound; change URL; ApplyProxyURLToChannels; assert channel proxy updated
}
```

- [ ] **Step 3: Implement bind helpers**

- Scan channels where `setting LIKE '%proxy_id%'` then exact-match via `GetSetting().ProxyId` (cross-DB safe).
- `SetChannelsProxy(ids, proxyId, proxyURL)`: if proxyId>0 set both; else clear both.
- `ApplyProxyURLToChannels(proxyId, newURL)`: rewrite URL only; return oldURLs for cache invalidation.
- `ClearProxyFromChannels(proxyId)`: clear both fields.
- `ListChannelsByProxyID` → `[]ChannelSummaryForProxy{Id,Name,Type,Status,Group}`.
- Wire `attachProxyChannelCounts` into `ListProxies` / `GetAllActiveProxies`.

- [ ] **Step 4: Run tests + relaykit build**

```bash
go test ./model/ -run 'TestSetChannels|TestApplyProxy|TestProxy' -count=1
cd relaykit && GOWORK=off go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add model/proxy.go model/proxy_test.go relaykit/dto/channel_settings.go
git commit -m "feat(proxy): channel bind count and proxy URL sync helpers"
```

---

### Task 4: Fallback resolve + expiry sweep

**Files:**
- Modify: `model/proxy.go`
- Modify: `model/proxy_test.go`

**Interfaces:**
- Produces: `func ResolveProxyFallback(p *Proxy, now int64, visiting map[int]struct{}) (target *Proxy, clear bool, err error)`, `func SweepExpiredProxies() (int, error)`, `func StartProxyExpirySweep(interval time.Duration)`

- [ ] **Step 1: Write failing tests**

```go
func TestResolveProxyFallback_Direct(t *testing.T) {
	p := &Proxy{FallbackMode: ProxyFallbackDirect}
	target, clear, err := ResolveProxyFallback(p, common.GetTimestamp(), map[int]struct{}{})
	require.NoError(t, err)
	assert.True(t, clear)
	assert.Nil(t, target)
}

func TestResolveProxyFallback_ProxyChain(t *testing.T) {
	setupProxyTestDB(t)
	backup := &Proxy{Name: "b", Protocol: "http", Host: "b", Port: 1, Status: ProxyStatusActive}
	require.NoError(t, backup.Insert())
	primary := &Proxy{Name: "a", Protocol: "http", Host: "a", Port: 2, FallbackMode: ProxyFallbackProxy, BackupProxyId: backup.Id}
	target, clear, err := ResolveProxyFallback(primary, common.GetTimestamp(), map[int]struct{}{})
	require.NoError(t, err)
	assert.False(t, clear)
	require.NotNil(t, target)
	assert.Equal(t, backup.Id, target.Id)
}

func TestSweepExpiredProxies_RebindsChannels(t *testing.T) {
	setupProxyTestDB(t)
	// backup active; primary expired with fallback proxy; channel bound to primary
	// SweepExpiredProxies(); channel.proxy_id == backup.Id; primary.Status == expired
}
```

- [ ] **Step 2: Run — FAIL**

```bash
go test ./model/ -run 'TestResolveProxyFallback|TestSweepExpired' -count=1
```

- [ ] **Step 3: Implement** (port from sub2api `proxy_fallback.go` + expiry service)

- Walk backup chain; skip expired/inactive; detect cycles via `visiting`
- Sweep: query active with `expires_at > 0 AND expires_at < now`; apply clear/rebind; mark expired
- `StartProxyExpirySweep`: ticker goroutine logging via `common.SysLog` / `SysError`

- [ ] **Step 4: PASS + commit**

```bash
go test ./model/ -run 'TestResolve|TestSweep|TestProxy' -count=1
git add model/proxy.go model/proxy_test.go
git commit -m "feat(proxy): expiry fallback resolve and sweep"
```

---

### Task 5: Probe + quality service

**Files:**
- Create: `service/proxy_probe.go`
- Create: `service/proxy_probe_test.go`

**Interfaces:**
- Produces:
  - `type ProxyTestResult struct { Success bool; Message string; LatencyMs int64; IPAddress, City, Region, Country, CountryCode string }`
  - `type ProxyQualityCheckResult` + `ProxyQualityCheckItem` (fields match design / sub2api FE)
  - `func ProbeProxyExit(ctx context.Context, proxyURL string) (*ProxyExitInfo, int64, error)`
  - `func TestManagedProxy(ctx context.Context, id int) (*ProxyTestResult, error)` — loads proxy, probes, persists via `UpdateProbeResult`
  - `func CheckManagedProxyQuality(ctx context.Context, id int) (*ProxyQualityCheckResult, error)`
  - Pure: `FinalizeProxyQualityResult`, `ProxyQualityGrade`

- [ ] **Step 1: Write pure scoring tests**

```go
func TestProxyQualityGrade(t *testing.T) {
	assert.Equal(t, "A", ProxyQualityGrade(90))
	assert.Equal(t, "B", ProxyQualityGrade(75))
	assert.Equal(t, "F", ProxyQualityGrade(10))
}

func TestFinalizeProxyQualityResult(t *testing.T) {
	r := &ProxyQualityCheckResult{PassedCount: 2, WarnCount: 1, FailedCount: 1, ChallengeCount: 0}
	FinalizeProxyQualityResult(r)
	assert.Equal(t, 100-10-22, r.Score)
	assert.Equal(t, "C", r.Grade) // 68
}
```

- [ ] **Step 2: Port quality targets + finalize from sub2api `admin_service.go` / `admin_proxy.go`**

Copy target URLs, allowed statuses, score formula, grade bands, overall status, CF challenge detection logic. Use `service.GetHttpClientWithProxy(proxyURL)` for requests. Use `common.Unmarshal` for JSON exit IP responses.

Probe flow (from `proxy_probe_service.go`):

1. Build client with proxy
2. Try `http://ip-api.com/json/?lang=zh-CN` then `http://api64.ipify.org?format=json`
3. Measure latency; parse IP/geo

`TestManagedProxy` / `CheckManagedProxyQuality` load by id, on probe error return result with Success=false (not error), still persist failed latency status.

- [ ] **Step 3: Run tests**

```bash
go test ./service/ -run 'TestProxyQuality|TestFinalize' -count=1
```

- [ ] **Step 4: Commit**

```bash
git add service/proxy_probe.go service/proxy_probe_test.go
git commit -m "feat(proxy): port connectivity probe and quality check"
```

---

### Task 6: Admin proxy controller + routes

**Files:**
- Create: `controller/proxy.go`
- Create: `router/proxy-router.go`
- Modify: `router/api-router.go` — call `registerProxyRoutes(apiRouter)`
- Create: `controller/proxy_test.go` (optional smoke for validate request binding)

**Interfaces:**
- Routes under `/api/proxy` with `middleware.AdminAuth()`
- Handlers as design §5

- [ ] **Step 1: Implement `registerProxyRoutes`**

```go
func registerProxyRoutes(apiRouter *gin.RouterGroup) {
	r := apiRouter.Group("/proxy")
	r.Use(middleware.AdminAuth())
	r.GET("/", controller.GetAllProxies)
	r.GET("/all", controller.GetAllProxiesNoPage)
	r.GET("/data", controller.ExportProxies)
	r.POST("/data", controller.ImportProxies)
	r.POST("/batch", controller.BatchCreateProxies)
	r.POST("/batch-delete", controller.BatchDeleteProxies)
	r.GET("/:id", controller.GetProxy)
	r.POST("/", controller.CreateProxy)
	r.PUT("/:id", controller.UpdateProxy)
	r.DELETE("/:id", controller.DeleteProxy)
	r.POST("/:id/test", controller.TestProxy)
	r.POST("/:id/quality-check", controller.CheckProxyQuality)
	r.GET("/:id/channels", controller.GetProxyChannels)
}
```

**Note:** Register static paths (`/all`, `/data`, `/batch`, `/batch-delete`) before `/:id` routes.

- [ ] **Step 2: Implement handlers**

Patterns from `controller/redemption.go`:

- List: `pageInfo := common.GetPageQuery(c)`; `model.ListProxies`; `pageInfo.SetItems`; `ApiSuccess`
- Create: bind JSON body (name, protocol, host, port, username, password, expires_at, fallback_mode, backup_proxy_id, expiry_warn_days); `Insert`; `ApiSuccess`
- Update: load existing; apply fields; if connection fields changed: `ApplyProxyURLToChannels` + `service.InvalidateProxyClient` for each old URL + new URL; `Update`
- Delete: `CountChannelsByProxyID` > 0 → `ApiErrorMsg` "proxy is in use"; else delete + invalidate
- Batch create: loop CheckProxyExists → skip or Insert name `"default"` if empty; return `{created,skipped}`
- Batch delete: per-id skip in-use; return `{deleted_ids, skipped}`
- Test/Quality: call service; `ApiSuccess(c, result)` even when `result.Success==false`
- Export/Import: implement Task 7 if not ready — **include minimal stubs only if splitting; prefer implement export/import in this task**

Request body structs:

```go
type proxyRequest struct {
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Status         string `json:"status"`
	ExpiresAt      int64  `json:"expires_at"`
	FallbackMode   string `json:"fallback_mode"`
	BackupProxyId  int    `json:"backup_proxy_id"`
	ExpiryWarnDays int    `json:"expiry_warn_days"`
}
```

- [ ] **Step 3: Manual compile**

```bash
go build -o /dev/null .
```

- [ ] **Step 4: Commit**

```bash
git add controller/proxy.go router/proxy-router.go router/api-router.go controller/proxy_test.go
git commit -m "feat(proxy): admin proxy API and routes"
```

---

### Task 7: Import / export data

**Files:**
- Modify: `controller/proxy.go` (or `controller/proxy_data.go`)
- Modify: `model/proxy.go` — `ProxyKey`, `ProxyExportItem` if not present
- Test: `controller/proxy_data_test.go` or `model/proxy_import_test.go`

- [ ] **Step 1: Define payload types** matching design §5.3

```go
type ProxyDataPayload struct {
	Type       string            `json:"type"` // "new-api-proxies"
	Version    int               `json:"version"`
	ExportedAt string            `json:"exported_at"`
	Proxies    []ProxyExportItem `json:"proxies"`
}
```

- [ ] **Step 2: Export**

- Query by `ids` csv or same filters as list
- Map each proxy to export item; `backup_proxy_name` from backup id lookup
- `proxy_key = ProxyKey(protocol,host,port,username,password)`

- [ ] **Step 3: Import**

- Bind `{ data: ProxyDataPayload }`
- For each item: if exists by key → reuse/update status+expiry+fallback; else create
- Second pass resolve `backup_proxy_name` → id; on miss set mode none + errors entry
- Return `{ proxy_created, proxy_reused, proxy_failed, errors: [...] }`

- [ ] **Step 4: Test key match + create**

```go
func TestProxyKeyStable(t *testing.T) {
	assert.Equal(t, "http|h|1|u|p", model.ProxyKey("HTTP", "h", 1, "u", "p"))
}
```

- [ ] **Step 5: Commit**

```bash
git add controller/proxy.go controller/proxy_data.go model/proxy.go
git commit -m "feat(proxy): import and export proxy data payload"
```

---

### Task 8: Channel save normalize + batch set proxy API

**Files:**
- Modify: `controller/channel.go` — after unmarshal settings / in `validateChannel` path
- Modify: `router/channel-router.go` — `POST /batch/proxy` with `ChannelWrite`
- Modify: `model/channel.go` only if helper needed
- Test: `controller/channel_proxy_test.go`

**Interfaces:**
- Produces: `BatchSetChannelProxy`, `normalizeChannelProxySettings(setting *dto.ChannelSettings) error`

- [ ] **Step 1: Write failing tests**

```go
func TestNormalizeChannelProxySettings_Managed(t *testing.T) {
	// setup DB with active proxy id=1
	s := dto.ChannelSettings{ProxyId: 1, Proxy: "should-be-overwritten"}
	require.NoError(t, normalizeChannelProxySettings(&s))
	assert.Equal(t, "http://...", s.Proxy) // proxy.URL()
}

func TestNormalizeChannelProxySettings_CustomURL(t *testing.T) {
	s := dto.ChannelSettings{ProxyId: 0, Proxy: "socks5://h:1080"}
	require.NoError(t, normalizeChannelProxySettings(&s))
}

func TestNormalizeChannelProxySettings_InvalidManaged(t *testing.T) {
	s := dto.ChannelSettings{ProxyId: 99999}
	require.Error(t, normalizeChannelProxySettings(&s))
}
```

- [ ] **Step 2: Implement normalize**

```go
func normalizeChannelProxySettings(s *dto.ChannelSettings) error {
	if s == nil {
		return nil
	}
	if s.ProxyId > 0 {
		p, err := model.GetProxyById(s.ProxyId)
		if err != nil {
			return fmt.Errorf("proxy not found")
		}
		// Prefer active for new binds; allow inactive if already bound only if you document — design: reject if not found; require active for bind
		if p.Status != model.ProxyStatusActive && !p.IsExpired(common.GetTimestamp()) {
			// if expired status or inactive: reject new bind
		}
		if p.Status != model.ProxyStatusActive {
			return fmt.Errorf("proxy is not active")
		}
		s.Proxy = p.URL()
		return nil
	}
	s.ProxyId = 0
	if _, err := common.ParseProxyURLStrict(s.Proxy); err != nil {
		return fmt.Errorf("invalid channel proxy: %w", err)
	}
	return nil
}
```

Call from `validateChannel` after unmarshaling settings (or inside `channel.ValidateSettings` after adding ProxyId handling — prefer controller/service next to existing validate to keep relaykit free of model imports).

**Important:** `ValidateSettings` lives in `model` and can call `GetProxyById` — OK in model package.

- [ ] **Step 3: BatchSetChannelProxy**

```go
type ChannelProxyBatch struct {
	Ids     []int `json:"ids"`
	ProxyId int   `json:"proxy_id"`
}
```

- Load proxy URL if ProxyId>0; `SetChannelsProxy`; invalidate old/new clients; `InitChannelCache` if needed; audit `channel.proxy_batch_set`

- [ ] **Step 4: Wire route**

```go
{method: http.MethodPost, path: "/batch/proxy", permission: authz.ChannelWrite, handler: controller.BatchSetChannelProxy},
```

- [ ] **Step 5: Tests + commit**

```bash
go test ./controller/ -run 'Proxy|ChannelProxy' -count=1
git add controller/channel.go controller/channel_proxy_test.go model/channel.go router/channel-router.go
git commit -m "feat(channel): normalize proxy_id and batch set proxy"
```

---

### Task 9: Start expiry sweep in main

**Files:**
- Modify: `main.go`

- [ ] **Step 1: After DB init / beside other background jobs**, add:

```go
model.StartProxyExpirySweep(time.Hour)
```

- [ ] **Step 2: Compile**

```bash
go build -o /dev/null .
```

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat(proxy): start background proxy expiry sweep"
```

---

### Task 10: Frontend feature scaffolding — types, API, route, sidebar

**Files:**
- Create: `web/src/features/proxies/types.ts`
- Create: `web/src/features/proxies/api.ts`
- Create: `web/src/features/proxies/constants.ts`
- Create: `web/src/features/proxies/index.tsx`
- Create: `web/src/routes/_authenticated/proxies/index.tsx`
- Modify: `web/src/hooks/use-sidebar-data.ts`
- Modify: `web/src/hooks/use-sidebar-config.ts`
- Modify: sidebar modules section if labels needed

- [ ] **Step 1: types + api** mirroring `redemption-codes`

```ts
export interface Proxy {
  id: number
  name: string
  protocol: 'http' | 'https' | 'socks5' | 'socks5h'
  host: string
  port: number
  username?: string
  password?: string
  status: 'active' | 'inactive' | 'expired'
  expires_at: number
  fallback_mode: 'none' | 'proxy' | 'direct'
  backup_proxy_id: number
  expiry_warn_days: number
  channel_count?: number
  latency_ms?: number | null
  latency_status?: string
  // ... probe/quality fields
  created_time: number
  updated_time: number
}
```

API functions: `getProxies`, `search/list with params`, `getAllProxies`, `createProxy`, `updateProxy`, `deleteProxy`, `batchCreateProxies`, `batchDeleteProxies`, `testProxy`, `checkProxyQuality`, `getProxyChannels`, `exportProxies`, `importProxies` → `/api/proxy/...`

- [ ] **Step 2: Route** like redemption-codes (`ROLE.ADMIN`, zod search page/filter/status)

- [ ] **Step 3: Sidebar**

```ts
{
  title: t('IP Management'),
  url: '/proxies',
  icon: Network, // from lucide-react
},
```

Config: `admin: { ..., proxies: true }`, `'/proxies': { section: 'admin', module: 'proxies' }`

- [ ] **Step 4: Minimal page shell**

```tsx
export function Proxies() {
  const { t } = useTranslation()
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('IP Management')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>{/* table later */}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
```

- [ ] **Step 5: Commit**

```bash
git add web/src/features/proxies web/src/routes/_authenticated/proxies web/src/hooks/use-sidebar-data.ts web/src/hooks/use-sidebar-config.ts
git commit -m "feat(web): scaffold proxies feature route and sidebar"
```

---

### Task 11: Proxies table, mutate drawer, bulk delete

**Files:**
- Create under `web/src/features/proxies/components/`: provider, table, columns, primary-buttons, mutate-drawer, delete-dialog, bulk-actions, dialogs
- Pattern: copy structure from `web/src/features/redemption-codes/components/*` and adapt fields

- [ ] **Step 1: Provider** — open dialog type, currentRow, refreshTrigger

- [ ] **Step 2: Columns** — select, name, protocol badge, address+copy, auth mask, location, channel_count button, latency/quality, expiry, status, row actions (test/quality/edit/delete stubs calling api)

- [ ] **Step 3: Table** — useQuery with page/filter/protocol/status; DataTablePage

- [ ] **Step 4: Mutate drawer** — RHF+zod:

Fields: name, protocol select, host, port, username, password, expires_at (datetime + presets 7/30/90), fallback_mode, backup_proxy_id (select from getAllProxies excluding self), expiry_warn_days, status (edit only)

Create mode tabs: Standard | Batch paste (`protocol://user:pass@host:port` lines) → `batchCreateProxies`

- [ ] **Step 5: Bulk delete** confirm → `batchDeleteProxies` toast skipped reasons

- [ ] **Step 6: Smoke**

```bash
cd web && bun run build
```

- [ ] **Step 7: Commit**

```bash
git add web/src/features/proxies
git commit -m "feat(web): proxies table CRUD and batch create/delete"
```

---

### Task 12: Test, quality, channels modal, import/export UI

**Files:**
- Modify proxies components
- Create: quality-report-dialog, channels-dialog, import-export dialogs

- [ ] **Step 1: Row + bulk test** — call `testProxy`, toast latency/country, triggerRefresh

- [ ] **Step 2: Quality** — call `checkProxyQuality`, open dialog with score/grade/items table

- [ ] **Step 3: Channel count click** — fetch `getProxyChannels`, list id/name/type/status

- [ ] **Step 4: Export** — download JSON from `exportProxies` (selected ids or filters)

- [ ] **Step 5: Import** — file/textarea JSON → `importProxies` → show created/reused/errors

- [ ] **Step 6: Commit**

```bash
git add web/src/features/proxies
git commit -m "feat(web): proxy test, quality, import/export UI"
```

---

### Task 13: Channel form — pool select + custom URL

**Files:**
- Modify: `web/src/features/channels/lib/channel-form.ts`
- Modify: `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- Modify: `web/src/features/channels/types.ts` if needed

- [ ] **Step 1: Form schema**

Add `proxy_id: z.number().optional().default(0)`  
Keep `proxy` with `isOptionalProxyURL` refine.

When loading channel: parse `proxy_id` from setting JSON.

- [ ] **Step 2: buildSettingJSON**

```ts
proxy: formData.proxy?.trim() || '',
proxy_id: formData.proxy_id || 0,
```

- [ ] **Step 3: UI block**

- Combobox: options from `getAllProxies()` + `{ id: 0, label: t('No proxy') }`
- If `proxy_id > 0`: set proxy field to display URL read-only (or hide free-text)
- If `proxy_id === 0`: show free-text Proxy Address input for custom URL
- Optional: small test button reusing `testProxy` when managed id selected

- [ ] **Step 4: Commit**

```bash
git add web/src/features/channels
git commit -m "feat(channels): select managed proxy or custom URL"
```

---

### Task 14: Channel bulk set proxy

**Files:**
- Modify: `web/src/features/channels/api.ts` — `batchSetChannelProxy(ids, proxyId)`
- Modify: `web/src/features/channels/lib/channel-actions.ts`
- Modify: `web/src/features/channels/components/data-table-bulk-actions.tsx`

- [ ] **Step 1: API**

```ts
export async function batchSetChannelProxy(ids: number[], proxy_id: number) {
  const res = await api.post('/api/channel/batch/proxy', { ids, proxy_id })
  return res.data
}
```

- [ ] **Step 2: Bulk action button** "Set proxy" → Dialog with proxy combobox (incl. clear) → submit → invalidate queries

- [ ] **Step 3: Commit**

```bash
git add web/src/features/channels
git commit -m "feat(channels): batch set proxy on selected channels"
```

---

### Task 15: i18n strings

**Files:**
- Modify: `web/src/i18n/locales/en.json`, `zh.json`, and other locales via skill/workflow

- [ ] **Step 1: Add English keys used in UI**

Examples: `IP Management`, `No proxy`, `Create Proxy`, `Batch Add Proxies`, `Test Connection`, `Quality Check`, `Proxy is in use`, `Set Proxy`, `Custom Proxy URL`, `Fallback Mode`, `Backup Proxy`, `Never expires`, etc.

- [ ] **Step 2: Run i18n sync if project script exists**

```bash
cd web && bun run i18n:sync
```

Or manually fill zh (and others) with proper translations.

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n
git commit -m "i18n: add IP management and channel proxy strings"
```

---

### Task 16: End-to-end verification

**Files:** none (verification only)

- [ ] **Step 1: Backend**

```bash
go test ./model/ -run Proxy -count=1
go test ./service/ -run Proxy -count=1
go test ./controller/ -run Proxy -count=1
go build -o /dev/null .
cd relaykit && GOWORK=off go build ./...
```

- [ ] **Step 2: Frontend**

```bash
cd web && bun run build
```

- [ ] **Step 3: Manual checklist against design success criteria**

- [ ] Sidebar IP 管理 visible for admin
- [ ] Create proxy standard + batch URL
- [ ] Test + quality update list fields
- [ ] Edit proxy updates bound channel setting.proxy
- [ ] Delete blocked when channel bound
- [ ] Channel form pool vs custom URL
- [ ] Batch set proxy on channels
- [ ] Import/export round-trip
- [ ] Expired proxy + fallback direct/proxy (can unit-test only if no clock control in UI)

- [ ] **Step 4: Final commit if fixes needed**; otherwise done.

---

## Spec Coverage Checklist

| Spec requirement | Task |
|------------------|------|
| proxies table + AutoMigrate | 1–2 |
| URL/Validate | 1 |
| CRUD + list filters | 2, 6 |
| channel_count / channels list | 3, 6, 12 |
| proxy_id + resolve URL | 3, 8, 13 |
| custom URL kept | 8, 13 |
| auto-sync on proxy update | 6 (Update handler) |
| connectivity test | 5, 6, 12 |
| quality check | 5, 6, 12 |
| batch create/delete | 6, 11 |
| delete in-use guard | 6 |
| expiry + fallback + sweep | 4, 9 |
| import/export | 7, 12 |
| batch channel proxy | 8, 14 |
| sidebar IP 管理 | 10 |
| new-api UI patterns | 11–14 |
| i18n | 15 |
| port from sub2api | 4, 5, 7 |
| tests | 1–5, 8, 16 |

## Self-Review Notes

- No TBD placeholders left in tasks.
- Types: `ProxyId` / `proxy_id` consistent FE/BE; status strings `active|inactive|expired`.
- Quality scoring numbers match sub2api (warn*10, fail*22, challenge*30).
- Route order: static paths before `/:id`.
- relaykit only gains a field; no model imports inside relaykit.
