package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

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

type proxyBatchCreateRequest struct {
	Proxies []proxyRequest `json:"proxies"`
}

type proxyBatchDeleteRequest struct {
	Ids []int `json:"ids"`
}

type proxyBatchDeleteSkipped struct {
	Id     int    `json:"id"`
	Reason string `json:"reason"`
}

// GetAllProxies returns a paginated proxy list.
func GetAllProxies(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filters := model.ProxyListFilters{
		Protocol:  c.Query("protocol"),
		Status:    c.Query("status"),
		Search:    strings.TrimSpace(c.Query("search")),
		SortBy:    c.DefaultQuery("sort_by", "id"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}
	if len(filters.Search) > 100 {
		filters.Search = filters.Search[:100]
	}
	list, total, err := model.ListProxies(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), filters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(list)
	common.ApiSuccess(c, pageInfo)
}

// GetAllProxiesNoPage returns active proxies for selectors.
func GetAllProxiesNoPage(c *gin.Context) {
	list, err := model.GetAllActiveProxies()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, list)
}

// GetProxy returns a single proxy by id.
func GetProxy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	proxy, err := model.GetProxyById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, proxy)
}

// CreateProxy inserts a managed proxy.
func CreateProxy(c *gin.Context) {
	var req proxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	proxy := &model.Proxy{
		Name:           req.Name,
		Protocol:       req.Protocol,
		Host:           req.Host,
		Port:           req.Port,
		Username:       req.Username,
		Password:       req.Password,
		Status:         req.Status,
		ExpiresAt:      req.ExpiresAt,
		FallbackMode:   req.FallbackMode,
		BackupProxyId:  req.BackupProxyId,
		ExpiryWarnDays: req.ExpiryWarnDays,
	}
	if err := proxy.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "proxy.create", map[string]interface{}{
		"id":   proxy.Id,
		"name": proxy.Name,
	})
	common.ApiSuccess(c, proxy)
}

// UpdateProxy updates a managed proxy and syncs bound channel URLs when connection fields change.
func UpdateProxy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req proxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	existing, err := model.GetProxyById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	oldURL := existing.URL()

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Protocol != "" {
		existing.Protocol = req.Protocol
	}
	if req.Host != "" {
		existing.Host = req.Host
	}
	if req.Port != 0 {
		existing.Port = req.Port
	}
	// Username/password may be intentionally cleared; always apply when present in body.
	// Binding always fills strings; treat empty as clear only if fields were sent.
	// For admin form updates we apply all provided fields.
	existing.Username = req.Username
	existing.Password = req.Password
	if req.Status != "" {
		existing.Status = req.Status
	}
	existing.ExpiresAt = req.ExpiresAt
	if req.FallbackMode != "" {
		existing.FallbackMode = req.FallbackMode
	}
	existing.BackupProxyId = req.BackupProxyId
	if req.ExpiryWarnDays != 0 {
		existing.ExpiryWarnDays = req.ExpiryWarnDays
	}

	if err := existing.Update(); err != nil {
		common.ApiError(c, err)
		return
	}

	newURL := existing.URL()
	if oldURL != newURL {
		oldURLs, applyErr := model.ApplyProxyURLToChannels(existing.Id, newURL)
		if applyErr != nil {
			common.ApiError(c, applyErr)
			return
		}
		for _, u := range oldURLs {
			service.InvalidateProxyClient(u)
		}
		service.InvalidateProxyClient(newURL)
		if common.MemoryCacheEnabled {
			model.InitChannelCache()
		}
	}

	recordManageAudit(c, "proxy.update", map[string]interface{}{
		"id": existing.Id,
	})
	common.ApiSuccess(c, existing)
}

// DeleteProxy deletes a proxy if no channels are bound.
func DeleteProxy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	proxy, err := model.GetProxyById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	count, err := model.CountChannelsByProxyID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if count > 0 {
		common.ApiErrorMsg(c, "proxy is in use")
		return
	}
	if err := model.DeleteProxyById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateProxyClient(proxy.URL())
	recordManageAudit(c, "proxy.delete", map[string]interface{}{
		"id": id,
	})
	common.ApiSuccess(c, nil)
}

// BatchCreateProxies creates proxies, skipping duplicates by host/port/auth.
func BatchCreateProxies(c *gin.Context) {
	var req proxyBatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	created := 0
	skipped := 0
	for _, item := range req.Proxies {
		exists, err := model.CheckProxyExists(item.Host, item.Port, item.Username, item.Password)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if exists {
			skipped++
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "default"
		}
		p := &model.Proxy{
			Name:           name,
			Protocol:       item.Protocol,
			Host:           item.Host,
			Port:           item.Port,
			Username:       item.Username,
			Password:       item.Password,
			Status:         item.Status,
			ExpiresAt:      item.ExpiresAt,
			FallbackMode:   item.FallbackMode,
			BackupProxyId:  item.BackupProxyId,
			ExpiryWarnDays: item.ExpiryWarnDays,
		}
		if err := p.Insert(); err != nil {
			common.ApiError(c, err)
			return
		}
		created++
	}
	recordManageAudit(c, "proxy.batch_create", map[string]interface{}{
		"created": created,
		"skipped": skipped,
	})
	common.ApiSuccess(c, gin.H{
		"created": created,
		"skipped": skipped,
	})
}

// BatchDeleteProxies deletes proxies, skipping those still bound to channels.
func BatchDeleteProxies(c *gin.Context) {
	var req proxyBatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	deletedIDs := make([]int, 0)
	skipped := make([]proxyBatchDeleteSkipped, 0)
	for _, id := range req.Ids {
		proxy, err := model.GetProxyById(id)
		if err != nil {
			skipped = append(skipped, proxyBatchDeleteSkipped{Id: id, Reason: err.Error()})
			continue
		}
		count, err := model.CountChannelsByProxyID(id)
		if err != nil {
			skipped = append(skipped, proxyBatchDeleteSkipped{Id: id, Reason: err.Error()})
			continue
		}
		if count > 0 {
			skipped = append(skipped, proxyBatchDeleteSkipped{Id: id, Reason: "proxy is in use"})
			continue
		}
		if err := model.DeleteProxyById(id); err != nil {
			skipped = append(skipped, proxyBatchDeleteSkipped{Id: id, Reason: err.Error()})
			continue
		}
		service.InvalidateProxyClient(proxy.URL())
		deletedIDs = append(deletedIDs, id)
	}
	recordManageAudit(c, "proxy.batch_delete", map[string]interface{}{
		"deleted": len(deletedIDs),
		"skipped": len(skipped),
	})
	common.ApiSuccess(c, gin.H{
		"deleted_ids": deletedIDs,
		"skipped":     skipped,
	})
}

// TestProxy runs connectivity probe for a managed proxy.
func TestProxy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.TestManagedProxy(c.Request.Context(), id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// CheckProxyQuality runs multi-target quality check for a managed proxy.
func CheckProxyQuality(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.CheckManagedProxyQuality(c.Request.Context(), id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// GetProxyChannels lists channels bound to a proxy.
func GetProxyChannels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channels, err := model.ListChannelsByProxyID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, channels)
}

// --- Import / export (Task 7) ---

const (
	proxyDataType    = "new-api-proxies"
	proxyDataVersion = 1
)

// ProxyExportItem is one proxy entry in the export payload.
type ProxyExportItem struct {
	ProxyKey        string `json:"proxy_key"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Status          string `json:"status"`
	ExpiresAt       int64  `json:"expires_at"`
	FallbackMode    string `json:"fallback_mode,omitempty"`
	BackupProxyName string `json:"backup_proxy_name,omitempty"`
	ExpiryWarnDays  int    `json:"expiry_warn_days,omitempty"`
}

// ProxyDataPayload is the import/export envelope.
type ProxyDataPayload struct {
	Type       string            `json:"type"`
	Version    int               `json:"version"`
	ExportedAt string            `json:"exported_at"`
	Proxies    []ProxyExportItem `json:"proxies"`
}

type proxyImportRequest struct {
	Data ProxyDataPayload `json:"data"`
}

type proxyImportError struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ProxyKey string `json:"proxy_key,omitempty"`
	Message  string `json:"message"`
}

type proxyImportResult struct {
	ProxyCreated int                `json:"proxy_created"`
	ProxyReused  int                `json:"proxy_reused"`
	ProxyFailed  int                `json:"proxy_failed"`
	Errors       []proxyImportError `json:"errors,omitempty"`
}

// ExportProxies exports proxies by ids or current list filters.
func ExportProxies(c *gin.Context) {
	ids, err := parseProxyIDsQuery(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	var proxies []*model.Proxy
	if len(ids) > 0 {
		proxies, err = model.GetProxiesByIds(ids)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		filters := model.ProxyListFilters{
			Protocol:  c.Query("protocol"),
			Status:    c.Query("status"),
			Search:    strings.TrimSpace(c.Query("search")),
			SortBy:    c.DefaultQuery("sort_by", "id"),
			SortOrder: c.DefaultQuery("sort_order", "desc"),
		}
		// Page through all matching rows.
		const pageSize = 500
		start := 0
		for {
			page, total, listErr := model.ListProxies(start, pageSize, filters)
			if listErr != nil {
				common.ApiError(c, listErr)
				return
			}
			proxies = append(proxies, page...)
			start += pageSize
			if int64(len(proxies)) >= total || len(page) == 0 {
				break
			}
		}
	}

	nameByID := make(map[int]string, len(proxies))
	for _, p := range proxies {
		if p != nil {
			nameByID[p.Id] = p.Name
		}
	}

	items := make([]ProxyExportItem, 0, len(proxies))
	for _, p := range proxies {
		if p == nil {
			continue
		}
		backupName := ""
		if p.BackupProxyId > 0 {
			if n, ok := nameByID[p.BackupProxyId]; ok {
				backupName = n
			} else if b, berr := model.GetProxyById(p.BackupProxyId); berr == nil {
				backupName = b.Name
			}
		}
		items = append(items, ProxyExportItem{
			ProxyKey:        model.ProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password),
			Name:            p.Name,
			Protocol:        p.Protocol,
			Host:            p.Host,
			Port:            p.Port,
			Username:        p.Username,
			Password:        p.Password,
			Status:          p.Status,
			ExpiresAt:       p.ExpiresAt,
			FallbackMode:    p.FallbackMode,
			BackupProxyName: backupName,
			ExpiryWarnDays:  p.ExpiryWarnDays,
		})
	}

	payload := ProxyDataPayload{
		Type:       proxyDataType,
		Version:    proxyDataVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    items,
	}
	common.ApiSuccess(c, payload)
}

// ImportProxies imports proxy data payload (create or reuse by proxy_key).
func ImportProxies(c *gin.Context) {
	var req proxyImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateProxyDataHeader(req.Data); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	// Page through existing proxies for key matching.
	var allExisting []*model.Proxy
	{
		start := 0
		const pageSize = 500
		for {
			page, total, listErr := model.ListProxies(start, pageSize, model.ProxyListFilters{SortBy: "id", SortOrder: "desc"})
			if listErr != nil {
				common.ApiError(c, listErr)
				return
			}
			allExisting = append(allExisting, page...)
			start += pageSize
			if int64(len(allExisting)) >= total || len(page) == 0 {
				break
			}
		}
	}

	proxyByKey := make(map[string]*model.Proxy, len(allExisting))
	proxyNameToID := make(map[string]int, len(allExisting))
	for _, p := range allExisting {
		if p == nil {
			continue
		}
		key := model.ProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyByKey[key] = p
		if p.Name != "" {
			proxyNameToID[p.Name] = p.Id
		}
	}

	result := proxyImportResult{}
	// First pass: create or reuse by key (backup resolved in second pass for new names).
	type pendingBackup struct {
		proxyID int
		name    string
		key     string
	}
	pending := make([]pendingBackup, 0)

	for i := range req.Data.Proxies {
		item := req.Data.Proxies[i]
		key := item.ProxyKey
		if key == "" {
			key = model.ProxyKey(item.Protocol, item.Host, item.Port, item.Username, item.Password)
		}
		if err := validateProxyExportItem(item); err != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, proxyImportError{
				Kind: "proxy", Name: item.Name, ProxyKey: key, Message: err.Error(),
			})
			continue
		}

		normalizedStatus := normalizeImportProxyStatus(item.Status)
		if existingProxy, ok := proxyByKey[key]; ok {
			result.ProxyReused++
			existingProxy.ExpiresAt = item.ExpiresAt
			if item.FallbackMode != "" {
				existingProxy.FallbackMode = item.FallbackMode
			}
			if item.ExpiryWarnDays > 0 {
				existingProxy.ExpiryWarnDays = item.ExpiryWarnDays
			}
			if normalizedStatus != "" {
				existingProxy.Status = normalizedStatus
			}
			// Resolve backup if name present
			if item.BackupProxyName != "" {
				if bid, found := proxyNameToID[item.BackupProxyName]; found {
					existingProxy.BackupProxyId = bid
				} else {
					existingProxy.FallbackMode = model.ProxyFallbackNone
					existingProxy.BackupProxyId = 0
					result.Errors = append(result.Errors, proxyImportError{
						Kind: "proxy", Name: item.Name, ProxyKey: key,
						Message: fmt.Sprintf("backup_proxy_name %q not found, fallback_mode downgraded to none", item.BackupProxyName),
					})
				}
			}
			if err := existingProxy.Update(); err != nil {
				result.Errors = append(result.Errors, proxyImportError{
					Kind: "proxy", Name: item.Name, ProxyKey: key,
					Message: "update failed: " + err.Error(),
				})
			}
			continue
		}

		fallbackMode := item.FallbackMode
		backupID := 0
		if item.BackupProxyName != "" {
			if bid, found := proxyNameToID[item.BackupProxyName]; found {
				backupID = bid
			} else {
				// May be resolved after later creates; try second pass.
				fallbackMode = model.ProxyFallbackNone
			}
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "default"
		}
		created := &model.Proxy{
			Name:           name,
			Protocol:       item.Protocol,
			Host:           item.Host,
			Port:           item.Port,
			Username:       item.Username,
			Password:       item.Password,
			Status:         normalizedStatus,
			ExpiresAt:      item.ExpiresAt,
			FallbackMode:   fallbackMode,
			BackupProxyId:  backupID,
			ExpiryWarnDays: item.ExpiryWarnDays,
		}
		if err := created.Insert(); err != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, proxyImportError{
				Kind: "proxy", Name: item.Name, ProxyKey: key, Message: err.Error(),
			})
			continue
		}
		result.ProxyCreated++
		proxyByKey[key] = created
		if created.Name != "" {
			proxyNameToID[created.Name] = created.Id
		}
		if item.BackupProxyName != "" && backupID == 0 {
			pending = append(pending, pendingBackup{proxyID: created.Id, name: item.BackupProxyName, key: key})
		}
	}

	// Second pass: resolve backup names that may have been created later in the batch.
	for _, p := range pending {
		bid, found := proxyNameToID[p.name]
		if !found {
			result.Errors = append(result.Errors, proxyImportError{
				Kind: "proxy", ProxyKey: p.key,
				Message: fmt.Sprintf("backup_proxy_name %q not found, fallback_mode downgraded to none", p.name),
			})
			continue
		}
		proxy, err := model.GetProxyById(p.proxyID)
		if err != nil {
			continue
		}
		// Restore intended proxy fallback now that backup exists.
		proxy.FallbackMode = model.ProxyFallbackProxy
		proxy.BackupProxyId = bid
		if err := proxy.Update(); err != nil {
			result.Errors = append(result.Errors, proxyImportError{
				Kind: "proxy", ProxyKey: p.key, Message: "backup resolve update failed: " + err.Error(),
			})
		}
	}

	recordManageAudit(c, "proxy.import", map[string]interface{}{
		"created": result.ProxyCreated,
		"reused":  result.ProxyReused,
		"failed":  result.ProxyFailed,
	})
	common.ApiSuccess(c, result)
}

func parseProxyIDsQuery(c *gin.Context) ([]int, error) {
	values := c.QueryArray("ids")
	if len(values) == 0 {
		raw := strings.TrimSpace(c.Query("ids"))
		if raw != "" {
			values = []string{raw}
		}
	}
	if len(values) == 0 {
		return nil, nil
	}
	ids := make([]int, 0)
	for _, item := range values {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.Atoi(part)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid proxy id: %s", part)
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func validateProxyDataHeader(payload ProxyDataPayload) error {
	if payload.Type != "" && payload.Type != proxyDataType {
		return fmt.Errorf("unsupported data type: %s", payload.Type)
	}
	if payload.Version != 0 && payload.Version != proxyDataVersion {
		return fmt.Errorf("unsupported data version: %d", payload.Version)
	}
	if payload.Proxies == nil {
		return fmt.Errorf("proxies is required")
	}
	return nil
}

func validateProxyExportItem(item ProxyExportItem) error {
	if strings.TrimSpace(item.Protocol) == "" {
		return fmt.Errorf("proxy protocol is required")
	}
	if strings.TrimSpace(item.Host) == "" {
		return fmt.Errorf("proxy host is required")
	}
	if item.Port <= 0 || item.Port > 65535 {
		return fmt.Errorf("proxy port is invalid")
	}
	switch strings.ToLower(strings.TrimSpace(item.Protocol)) {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("proxy protocol is invalid: %s", item.Protocol)
	}
	return nil
}

func normalizeImportProxyStatus(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "", model.ProxyStatusActive:
		return model.ProxyStatusActive
	case model.ProxyStatusInactive:
		return model.ProxyStatusInactive
	case model.ProxyStatusExpired:
		// Import expired as inactive to avoid immediate re-route sweep side effects.
		return model.ProxyStatusInactive
	default:
		return model.ProxyStatusActive
	}
}
