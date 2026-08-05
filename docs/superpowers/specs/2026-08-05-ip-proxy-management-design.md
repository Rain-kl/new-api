# IP / Proxy Management Design

**Date:** 2026-08-05  
**Status:** Approved for spec (brainstorming)  
**Scope:** new-api only; reference implementation source is sub2api  
**Related:** sub2api `/admin/proxies`, new-api channel `setting.proxy`

## 1. Problem

Operators need a **managed outbound proxy pool** (IP management) so that:

1. Proxies are created, tested, and lifecycle-managed in one place.
2. Channels bind to a proxy without retyping URLs on every channel.
3. Multiple channels can be assigned or cleared in bulk.

**Today in new-api:**

| Piece | Behavior | Gap |
|-------|----------|-----|
| `ChannelSettings.Proxy` | Free-text URL in channel `setting` JSON | No pool, no shared edit, no bulk assign |
| `common.ParseProxyURLStrict` / `service.GetHttpClientWithProxy*` | Runtime HTTP client via proxy | Works only with raw URL strings |
| Admin UI | Proxy Address input on channel form | No IP management page |

**sub2api already has** a full proxy admin surface (`/admin/proxies`): CRUD, batch create/delete, connectivity test, multi-target quality check, expiry + fallback, import/export, account binding via `proxy_id`. That product logic is the reference; **UI must follow new-api** (React, data-table, Sheet/Dialog), not Vue copy-paste.

## 2. Goals and non-goals

### Goals

1. Admin **IP 管理** page in the sidebar (`/proxies`).
2. **Operational parity** with sub2api proxy management for:
   - CRUD, list filters/sort/pagination
   - Connectivity test (latency + exit geo)
   - Quality check (multi-target score/grade/report)
   - Batch create from URL lines; batch delete with in-use skip reasons
   - Expiry fields + `fallback_mode` (`none` \| `proxy` \| `direct`) + backup proxy
   - Background expiry sweep applying fallback to bound **channels**
   - Import/export JSON aligned with sub2api data payload shape (proxies portion)
   - View channels using a proxy
3. **Channel integration:**
   - Dropdown to pick a managed proxy (including “no proxy”)
   - **Keep free-text custom proxy URL** when not bound to the pool
   - Batch set/clear proxy on multiple channels
4. When a managed proxy’s connection fields change, **auto-sync** bound channels’ resolved `setting.proxy` and invalidate proxy HTTP client cache.
5. Prefer **porting** sub2api probe / quality / import-export / expiry logic into new-api packages rather than inventing new algorithms.
6. Runtime outbound path continues to use the existing `ChannelSettings.Proxy` string (minimal relay risk).

### Non-goals

- sub2api ad banners, mock stats endpoint, OAuth-on-proxy for account login flows.
- sub2api account / spark-shadow inheritance rules.
- Changing how non-channel HTTP clients work except through existing channel proxy settings.
- Full soft multi-tenant proxy ownership (global admin pool only).

## 3. Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Storage | New `proxies` table + `ChannelSettings.proxy_id` | First-class CRUD; channel reverse lookup for counts/sync |
| Runtime | Keep writing resolved URL into `setting.proxy` | No hot-path DB lookup; reuses client cache keys |
| Binding modes | Pool (`proxy_id > 0`) **or** custom URL (`proxy_id = 0` + `proxy` string) | Operator asked to keep hand-filled URLs |
| Delete in use | Block single delete; batch-delete skips with reason | Aligns with sub2api |
| Update pool entry | Rewrite all bound channels’ `proxy` URL | Avoid stale egress after admin edit |
| Implementation source | Port from `/Users/ryan/Code/Go/sub2api` | User request: don’t reinvent wheels |
| UI | new-api patterns (redemption-codes / channels) | Consistent admin UX |

## 4. Data model

### 4.1 Table `proxies`

GORM model registered via `RegisterMainDBModel(&Proxy{})`. Soft delete with `gorm.DeletedAt`.

| Field | Type | Notes |
|-------|------|--------|
| `id` | int PK | |
| `name` | varchar(100) | required |
| `protocol` | varchar(20) | `http` \| `https` \| `socks5` \| `socks5h` |
| `host` | varchar(255) | required |
| `port` | int | 1–65535 |
| `username` | varchar(100) | optional, default `''` |
| `password` | varchar(255) | optional, default `''` |
| `status` | varchar(20) | `active` \| `inactive` \| `expired`; default `active` |
| `expires_at` | bigint | unix seconds; `0` = never |
| `fallback_mode` | varchar(20) | `none` \| `proxy` \| `direct`; default `none` |
| `backup_proxy_id` | int | `0` if unused; self-ref when mode=`proxy` |
| `expiry_warn_days` | int | default `7` (no GORM `default:true` boolean issues) |
| Probe cache | latency_ms, latency_status, latency_message, ip_address, country, country_code, region, city | Updated by test/quality |
| Quality cache | quality_status, quality_score, quality_grade, quality_summary, quality_checked | Updated by quality-check |
| `created_time` / `updated_time` | bigint | |
| `channel_count` | int64 `gorm:"-"` | list enrichment only |

**URL construction** (port from sub2api `Proxy.URL()`):

- Scheme = protocol; host = `net.JoinHostPort(host, port)`.
- Userinfo only if **both** username and password non-empty.
- Result must pass `common.ParseProxyURLStrict`.

**Validation:**

- `fallback_mode=proxy` requires `backup_proxy_id > 0`.
- `backup_proxy_id` cannot equal self.
- `expiry_warn_days >= 0`; empty/zero on create defaults to `7`.

### 4.2 Channel settings

Extend `relaykit/dto.ChannelSettings`:

```go
Proxy   string `json:"proxy"`
ProxyId int    `json:"proxy_id,omitempty"` // 0 = not bound to pool
```

| Mode | `proxy_id` | `proxy` |
|------|------------|---------|
| No proxy | 0 | `""` |
| Managed pool | >0 | Resolved URL from proxies row (server-enforced) |
| Custom URL | 0 | Operator free-text URL (strict parse) |

**Save rules (server):**

1. If `proxy_id > 0`: load proxy; reject if missing or not usable for binding (at least must exist; prefer `active` for new binds); set `proxy = proxy.URL()`.
2. If `proxy_id == 0`: keep/clear `proxy` as submitted; validate with `ParseProxyURLStrict` if non-empty.
3. On managed proxy update (host/port/user/pass/protocol): for every channel with matching `proxy_id`, rewrite `setting.proxy` and collect old URLs for `service.InvalidateProxyClient`.

## 5. API

Base: `/api/proxy` with `middleware.AdminAuth()` (same bar as redemption codes).  
Response shape: existing `common.ApiSuccess` / `ApiError` (HTTP 200 business envelope).

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Paginated list; query: `p`, `page_size`, `protocol`, `status`, `search`, `sort_by`, `sort_order` |
| GET | `/all` | Active proxies for selectors; `with_count=true` optional |
| GET | `/:id` | Detail (includes password for admin edit form) |
| POST | `/` | Create |
| PUT | `/:id` | Update |
| DELETE | `/:id` | Delete; fail if any channel bound |
| POST | `/batch` | Batch create `{ proxies: [{protocol,host,port,username?,password?}] }` → `{created,skipped}` |
| POST | `/batch-delete` | `{ ids }` → `{ deleted_ids, skipped: [{id, reason}] }` |
| POST | `/:id/test` | Connectivity probe |
| POST | `/:id/quality-check` | Multi-target quality |
| GET | `/:id/channels` | Channel summaries using this proxy |
| GET | `/data` | Export payload |
| POST | `/data` | Import payload |

### 5.1 Channel batch

| Method | Path | Body | Purpose |
|--------|------|------|---------|
| POST | `/api/channel/batch/proxy` | `{ ids: int[], proxy_id: int }` | `proxy_id=0` clears pool bind + proxy URL; `>0` binds and resolves URL |

Single channel create/update already send `setting` JSON; server normalizes `proxy_id`/`proxy` as above.

### 5.2 Probe & quality (ported behavior)

**Test** (sub2api `TestProxy` / probe service):

1. Build client through proxy URL.
2. Hit exit-IP endpoints (ip-api / ipify fallback as in sub2api).
3. Return `success`, `message`, `latency_ms`, geo fields.
4. Persist probe fields on the proxy row for list display.

**Quality check** (sub2api targets + scoring):

1. Base connectivity item.
2. Targets: openai / anthropic / gemini / grok (same URLs and pass status sets as sub2api).
3. Status: pass / warn / fail / challenge (CF).
4. Score formula and grade bands ported from sub2api.
5. Persist quality snapshot fields on the proxy row.

### 5.3 Import / export

Export JSON (proxies portion aligned with sub2api):

```json
{
  "type": "new-api-proxies",
  "version": 1,
  "exported_at": "RFC3339",
  "proxies": [
    {
      "proxy_key": "protocol|host|port|username|password",
      "name": "...",
      "protocol": "http",
      "host": "...",
      "port": 8080,
      "username": "",
      "password": "",
      "status": "active",
      "expires_at": 0,
      "fallback_mode": "none",
      "backup_proxy_name": "",
      "expiry_warn_days": 7
    }
  ]
}
```

Import: match by `proxy_key`; create or reuse; resolve `backup_proxy_name` → id or downgrade to `none` with error entry.

### 5.4 Expiry sweep

Background ticker in `main` (interval ~1h, configurable via env if already patterned elsewhere; otherwise constant):

1. Find `status=active` with `expires_at > 0` and `expires_at < now`.
2. For bound channels:
   - `direct` → clear `proxy_id` + `proxy`
   - `proxy` → walk backup chain (skip expired/inactive/cycles) → rebind to target URL
   - `none` → leave channel bindings; mark proxy expired only
3. Set proxy `status=expired`.
4. Invalidate affected proxy client cache URLs.

## 6. Frontend

### 6.1 Navigation

- Sidebar Admin item: **IP 管理** → `/proxies` (e.g. `Network` / `Globe` icon).
- `use-sidebar-config`: `admin.proxies` module + `URL_TO_CONFIG_MAP['/proxies']`.
- Route: `web/src/routes/_authenticated/proxies/index.tsx` with `ROLE.ADMIN` gate.

### 6.2 Feature module `web/src/features/proxies/`

Mirror redemption-codes layout:

- `api.ts`, `types.ts`, `constants.ts`, `index.tsx`
- `components/`: provider, table, columns, primary buttons, mutate drawer/dialog, bulk actions, quality report dialog, channels dialog, import/export dialogs

**Table / toolbar capabilities** (logic parity with sub2api ProxiesView):

- Search; protocol filter; status filter (active/inactive/expired)
- Refresh; batch test; batch quality; batch delete; import; export; create
- Columns: select, name, protocol, address+copy, auth mask, location, channel count, latency/quality, expiry, status, actions
- Create: Standard form | Batch URL paste tabs
- Form fields: name, protocol, host, port, username, password, expires presets, fallback, backup proxy, expiry_warn_days, status (edit)

### 6.3 Channel UI changes

- Replace pure free-text proxy with:
  - Combobox/Select of managed proxies + “No proxy”
  - Toggle or secondary field for **Custom URL** when not using pool
  - When pool selected: show read-only resolved URL; set `proxy_id`
  - When custom: `proxy_id=0`, editable `proxy`
- Bulk actions: **Set proxy** dialog → pick proxy or clear
- Form serialize: `buildSettingJSON` includes `proxy_id` and `proxy`

### 6.4 i18n

English source keys; complete locales via project i18n workflow (`en`, `zh`, and remaining project languages).

## 7. Backend file map

| Unit | Responsibility |
|------|----------------|
| `model/proxy.go` | Entity, CRUD, list filters, channel count/bind/sync, expiry data ops |
| `model/proxy_test.go` | URL, validate, bind/sync, fallback resolve |
| `service/proxy_probe.go` | Test + quality (ported) |
| `controller/proxy.go` | HTTP handlers |
| `router/api-router.go` (or `proxy-router.go`) | Route registration |
| `relaykit/dto/channel_settings.go` | `ProxyId` field |
| `controller/channel.go` | Normalize setting on save; `BatchSetChannelProxy` |
| `main.go` | Start expiry sweep |
| `web/src/features/proxies/*` | Admin page |
| `web/src/features/channels/*` | Form + bulk |
| `web/src/hooks/use-sidebar-data.ts` / `use-sidebar-config.ts` | Nav |

## 8. Error handling

| Case | Behavior |
|------|----------|
| Validation errors | `success: false`, clear message |
| Delete while in use | Single: error; batch: skip + reason |
| Test/quality network fail | Always `ApiSuccess(c, result)` where `result` includes `success: false` and `message` (probe failure is a result, not an API error). FE keys off `data.success`. |
| Unknown `proxy_id` on channel save | Reject |
| Import backup name missing | Proxy imported with `fallback_mode=none`; error list entry |
| Custom URL invalid | Existing strict proxy validation |

## 9. Testing

Backend (`testify` require/assert):

1. `Proxy.URL()` — host:port, auth both-or-neither, IPv6 join.
2. Validate — protocol, port, fallback/backup/self.
3. Delete in-use blocked; batch delete partial success.
4. Channel resolve — `proxy_id` writes URL; custom path leaves `proxy_id=0`.
5. Update proxy syncs channel settings URLs.
6. Batch set channel proxy / clear.
7. Fallback resolve — direct, proxy chain, cycle guard.
8. Quality scoring pure function unit tests if scoring extracted.

Frontend: schema/type tests if existing channel form test style applies; no pure snapshot spam.

## 10. Implementation principles

1. **Port first** from sub2api (`admin_proxy.go`, probe service, quality targets, proxy_data import/export, proxy_fallback / expiry).
2. Adapt to new-api: GORM models, `common.*` JSON, gin response helpers, channel instead of account.
3. **YAGNI outside approved scope** — no ad banner, no mock stats, no OAuth proxy binding.
4. Cross-DB AutoMigrate only; no dialect-only SQL without fallbacks.
5. After design approval: **writing-plans** → task plan → implementation (TDD where tests listed).

## 11. Success criteria

- [ ] Admin can manage proxy pool end-to-end (CRUD, test, quality, batch, import/export, expiry fields).
- [ ] Sidebar **IP 管理** visible to admins; module toggle works.
- [ ] Channel form: select pool **or** custom URL; batch set proxy works.
- [ ] Bound channels track pool updates; delete-in-use is safe.
- [ ] Expiry sweep applies fallback correctly.
- [ ] Outbound relay still uses `setting.proxy` string path.
- [ ] UI matches new-api admin style; ops logic aligned with sub2api.

## 12. Open points resolved in brainstorming

| Topic | Resolution |
|-------|------------|
| Scope | Near-full sub2api ops parity (not minimal CRUD) |
| Custom URL | Keep |
| Architecture | Pool table + denormalized URL on channel settings |
| Sync on edit | Auto-update bound channels |
| Legacy data | Not a concern (no production proxy usage today) |
| Code origin | Port from sub2api, adapt UI to new-api |
