package model

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	ProxyStatusActive   = "active"
	ProxyStatusInactive = "inactive"
	ProxyStatusExpired  = "expired"

	ProxyFallbackNone   = "none"
	ProxyFallbackProxy  = "proxy"
	ProxyFallbackDirect = "direct"
)

// Proxy is a managed outbound proxy in the IP pool (table proxies).
type Proxy struct {
	Id             int            `json:"id"`
	Name           string         `json:"name" gorm:"type:varchar(100);not null"`
	Protocol       string         `json:"protocol" gorm:"type:varchar(20);not null"`
	Host           string         `json:"host" gorm:"type:varchar(255);not null"`
	Port           int            `json:"port"`
	Username       string         `json:"username" gorm:"type:varchar(100);default:''"`
	Password       string         `json:"password" gorm:"type:varchar(255);default:''"`
	Status         string         `json:"status" gorm:"type:varchar(20)"` // default active via Validate
	ExpiresAt      int64          `json:"expires_at" gorm:"bigint"`       // unix seconds; 0 = never
	FallbackMode   string         `json:"fallback_mode" gorm:"type:varchar(20)"`
	BackupProxyId  int            `json:"backup_proxy_id"`
	ExpiryWarnDays int            `json:"expiry_warn_days"`

	// Probe cache (updated by connectivity test / quality check)
	LatencyMs      int64  `json:"latency_ms" gorm:"bigint"`
	LatencyStatus  string `json:"latency_status" gorm:"type:varchar(50)"`
	LatencyMessage string `json:"latency_message" gorm:"type:text"`
	IpAddress      string `json:"ip_address" gorm:"type:varchar(100)"`
	Country        string `json:"country" gorm:"type:varchar(100)"`
	CountryCode    string `json:"country_code" gorm:"type:varchar(20)"`
	Region         string `json:"region" gorm:"type:varchar(100)"`
	City           string `json:"city" gorm:"type:varchar(100)"`

	// Quality cache
	QualityStatus  string `json:"quality_status" gorm:"type:varchar(50)"`
	QualityScore   int    `json:"quality_score"`
	QualityGrade   string `json:"quality_grade" gorm:"type:varchar(20)"`
	QualitySummary string `json:"quality_summary" gorm:"type:text"`
	QualityChecked int64  `json:"quality_checked" gorm:"bigint"`

	CreatedTime  int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime  int64          `json:"updated_time" gorm:"bigint"`
	ChannelCount int64          `json:"channel_count" gorm:"-"` // list enrichment only
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
}

func init() {
	RegisterMainDBModel(&Proxy{})
}

func (Proxy) TableName() string {
	return "proxies"
}

// URL builds the proxy connection URL. Userinfo is included only when both
// username and password are non-empty (sub2api compatibility).
func (p *Proxy) URL() string {
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

// Validate normalizes fields and checks invariants. Defaults status to active
// and expiry_warn_days to 7 when unset (zero).
func (p *Proxy) Validate() error {
	if p == nil {
		return errors.New("proxy is nil")
	}

	p.Name = strings.TrimSpace(p.Name)
	p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	p.Password = strings.TrimSpace(p.Password)
	p.Status = strings.ToLower(strings.TrimSpace(p.Status))
	p.FallbackMode = strings.ToLower(strings.TrimSpace(p.FallbackMode))

	if p.Name == "" {
		return errors.New("name is required")
	}
	if p.Host == "" {
		return errors.New("host is required")
	}
	switch p.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("protocol must be http, https, socks5, or socks5h")
	}
	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	if p.Status == "" {
		p.Status = ProxyStatusActive
	}
	switch p.Status {
	case ProxyStatusActive, ProxyStatusInactive, ProxyStatusExpired:
	default:
		return fmt.Errorf("status must be active, inactive, or expired")
	}

	if p.FallbackMode == "" {
		p.FallbackMode = ProxyFallbackNone
	}
	switch p.FallbackMode {
	case ProxyFallbackNone, ProxyFallbackProxy, ProxyFallbackDirect:
	default:
		return fmt.Errorf("fallback_mode must be none, proxy, or direct")
	}
	if p.FallbackMode == ProxyFallbackProxy && p.BackupProxyId <= 0 {
		return errors.New("backup_proxy_id is required when fallback_mode is proxy")
	}
	if p.Id > 0 && p.BackupProxyId > 0 && p.BackupProxyId == p.Id {
		return errors.New("backup_proxy_id cannot equal self")
	}

	if p.ExpiryWarnDays < 0 {
		return errors.New("expiry_warn_days must be >= 0")
	}
	if p.ExpiryWarnDays == 0 {
		p.ExpiryWarnDays = 7
	}

	if _, err := common.ParseProxyURLStrict(p.URL()); err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}
	return nil
}

// IsExpired reports whether expires_at is set and strictly before now.
func (p *Proxy) IsExpired(now int64) bool {
	if p == nil {
		return false
	}
	return p.ExpiresAt > 0 && p.ExpiresAt < now
}

// EffectiveStatus returns expired when status is expired or expires_at has passed.
func (p *Proxy) EffectiveStatus() string {
	if p == nil {
		return ""
	}
	if p.Status == ProxyStatusExpired || p.IsExpired(common.GetTimestamp()) {
		return ProxyStatusExpired
	}
	return p.Status
}

// ProxyListFilters controls ListProxies filtering and sort.
type ProxyListFilters struct {
	Protocol  string
	Status    string
	Search    string
	SortBy    string
	SortOrder string
}

var proxySortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"protocol":      "protocol",
	"host":          "host",
	"port":          "port",
	"status":        "status",
	"expires_at":    "expires_at",
	"latency_ms":    "latency_ms",
	"quality_score": "quality_score",
	"created_time":  "created_time",
	"updated_time":  "updated_time",
}

// Insert validates and creates a proxy row.
func (p *Proxy) Insert() error {
	if err := p.Validate(); err != nil {
		return err
	}
	now := common.GetTimestamp()
	p.CreatedTime = now
	p.UpdatedTime = now
	return DB.Create(p).Error
}

// Update validates and updates all mutable proxy columns.
func (p *Proxy) Update() error {
	if p == nil || p.Id <= 0 {
		return errors.New("invalid proxy id")
	}
	if err := p.Validate(); err != nil {
		return err
	}
	p.UpdatedTime = common.GetTimestamp()
	return DB.Model(p).Select(
		"name", "protocol", "host", "port", "username", "password",
		"status", "expires_at", "fallback_mode", "backup_proxy_id", "expiry_warn_days",
		"latency_ms", "latency_status", "latency_message",
		"ip_address", "country", "country_code", "region", "city",
		"quality_status", "quality_score", "quality_grade", "quality_summary", "quality_checked",
		"updated_time",
	).Updates(p).Error
}

// UpdateProbeResult persists only probe/quality cache fields.
func (p *Proxy) UpdateProbeResult() error {
	if p == nil || p.Id <= 0 {
		return errors.New("invalid proxy id")
	}
	p.UpdatedTime = common.GetTimestamp()
	return DB.Model(p).Select(
		"latency_ms", "latency_status", "latency_message",
		"ip_address", "country", "country_code", "region", "city",
		"quality_status", "quality_score", "quality_grade", "quality_summary", "quality_checked",
		"updated_time",
	).Updates(p).Error
}

// GetProxyById loads a non-deleted proxy by primary key.
func GetProxyById(id int) (*Proxy, error) {
	if id <= 0 {
		return nil, errors.New("invalid proxy id")
	}
	var p Proxy
	err := DB.First(&p, id).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteProxyById soft-deletes a proxy.
func DeleteProxyById(id int) error {
	if id <= 0 {
		return errors.New("invalid proxy id")
	}
	return DB.Delete(&Proxy{}, id).Error
}

// ListProxies returns a page of proxies with optional filters.
func ListProxies(start, num int, filters ProxyListFilters) ([]*Proxy, int64, error) {
	if num <= 0 {
		num = 10
	}
	if start < 0 {
		start = 0
	}

	query := DB.Model(&Proxy{})
	protocol := strings.ToLower(strings.TrimSpace(filters.Protocol))
	if protocol != "" {
		query = query.Where("protocol = ?", protocol)
	}

	now := common.GetTimestamp()
	status := strings.ToLower(strings.TrimSpace(filters.Status))
	switch status {
	case ProxyStatusActive:
		// Stored active and not past expires_at.
		query = query.Where("status = ?", ProxyStatusActive).
			Where("(expires_at = 0 OR expires_at >= ?)", now)
	case ProxyStatusInactive:
		query = query.Where("status = ?", ProxyStatusInactive)
	case ProxyStatusExpired:
		query = query.Where("status = ? OR (expires_at > 0 AND expires_at < ?)", ProxyStatusExpired, now)
	case "":
		// no status filter
	default:
		query = query.Where("status = ?", status)
	}

	search := strings.TrimSpace(filters.Search)
	if search != "" {
		like := "%" + search + "%"
		if id := common.String2Int(search); id > 0 {
			query = query.Where(
				"id = ? OR name LIKE ? OR host LIKE ? OR username LIKE ?",
				id, like, like, like,
			)
		} else {
			query = query.Where(
				"name LIKE ? OR host LIKE ? OR username LIKE ?",
				like, like, like,
			)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	sortBy := strings.ToLower(strings.TrimSpace(filters.SortBy))
	sortOrder := strings.ToLower(strings.TrimSpace(filters.SortOrder))
	column, ok := proxySortColumns[sortBy]
	if !ok {
		column = "id"
		sortOrder = "desc"
	} else if sortOrder != "asc" {
		sortOrder = "desc"
	}
	orderExpr := column + " " + sortOrder

	var list []*Proxy
	err := query.Order(orderExpr).Offset(start).Limit(num).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	attachProxyChannelCounts(list)
	return list, total, nil
}

// GetAllActiveProxies returns active proxies ordered by id desc.
func GetAllActiveProxies() ([]*Proxy, error) {
	var list []*Proxy
	err := DB.Where("status = ?", ProxyStatusActive).Order("id desc").Find(&list).Error
	if err != nil {
		return nil, err
	}
	attachProxyChannelCounts(list)
	return list, nil
}

// CheckProxyExists reports whether a proxy with the same host/port/auth exists.
func CheckProxyExists(host string, port int, username, password string) (bool, error) {
	var count int64
	err := DB.Model(&Proxy{}).
		Where("host = ? AND port = ? AND username = ? AND password = ?", host, port, username, password).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ProxyKey builds a stable identity key for import/export matching.
func ProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s",
		strings.ToLower(strings.TrimSpace(protocol)),
		strings.TrimSpace(host),
		port,
		strings.TrimSpace(username),
		strings.TrimSpace(password),
	)
}

// GetProxiesByIds loads proxies for the given ids (order not guaranteed).
func GetProxiesByIds(ids []int) ([]*Proxy, error) {
	if len(ids) == 0 {
		return []*Proxy{}, nil
	}
	var list []*Proxy
	err := DB.Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// ChannelSummaryForProxy is a lightweight channel row for proxy binding UIs.
type ChannelSummaryForProxy struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Type   int    `json:"type"`
	Status int    `json:"status"`
	Group  string `json:"group"`
}

// channelsLikelyBoundToProxy pre-filters channels that may reference proxy_id in setting JSON.
func channelsLikelyBoundToProxy() ([]Channel, error) {
	var channels []Channel
	// Cross-DB safe: LIKE then exact match via GetSetting().ProxyId.
	err := DB.Where("setting LIKE ?", "%proxy_id%").Find(&channels).Error
	return channels, err
}

func channelsBoundToProxyID(proxyId int) ([]Channel, error) {
	if proxyId <= 0 {
		return nil, nil
	}
	candidates, err := channelsLikelyBoundToProxy()
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0)
	for _, ch := range candidates {
		if ch.GetSetting().ProxyId == proxyId {
			out = append(out, ch)
		}
	}
	return out, nil
}

// CountChannelsByProxyID counts channels bound to the given managed proxy.
func CountChannelsByProxyID(proxyId int) (int64, error) {
	channels, err := channelsBoundToProxyID(proxyId)
	if err != nil {
		return 0, err
	}
	return int64(len(channels)), nil
}

// ListChannelsByProxyID returns channel summaries bound to the managed proxy.
func ListChannelsByProxyID(proxyId int) ([]ChannelSummaryForProxy, error) {
	channels, err := channelsBoundToProxyID(proxyId)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelSummaryForProxy, 0, len(channels))
	for _, ch := range channels {
		out = append(out, ChannelSummaryForProxy{
			Id:     ch.Id,
			Name:   ch.Name,
			Type:   ch.Type,
			Status: ch.Status,
			Group:  ch.Group,
		})
	}
	return out, nil
}

// SetChannelsProxy binds or clears proxy settings on the given channel IDs.
// When proxyId > 0 both ProxyId and Proxy URL are set; otherwise both are cleared.
// Returns the number of channels updated.
func SetChannelsProxy(ids []int, proxyId int, proxyURL string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var channels []Channel
	if err := DB.Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return 0, err
	}
	updated := 0
	for i := range channels {
		setting := channels[i].GetSetting()
		if proxyId > 0 {
			setting.ProxyId = proxyId
			setting.Proxy = proxyURL
		} else {
			setting.ProxyId = 0
			setting.Proxy = ""
		}
		channels[i].SetSetting(setting)
		if err := DB.Model(&channels[i]).Update("setting", channels[i].Setting).Error; err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

// ApplyProxyURLToChannels rewrites setting.proxy for all channels bound to proxyId.
// Returns the set of previous proxy URL strings (for cache invalidation).
func ApplyProxyURLToChannels(proxyId int, newURL string) ([]string, error) {
	channels, err := channelsBoundToProxyID(proxyId)
	if err != nil {
		return nil, err
	}
	oldURLs := make([]string, 0)
	seen := make(map[string]struct{})
	for i := range channels {
		setting := channels[i].GetSetting()
		old := setting.Proxy
		if old != "" {
			if _, ok := seen[old]; !ok {
				seen[old] = struct{}{}
				oldURLs = append(oldURLs, old)
			}
		}
		setting.ProxyId = proxyId
		setting.Proxy = newURL
		channels[i].SetSetting(setting)
		if err := DB.Model(&channels[i]).Update("setting", channels[i].Setting).Error; err != nil {
			return oldURLs, err
		}
	}
	return oldURLs, nil
}

// ClearProxyFromChannels clears proxy_id and proxy on all channels bound to proxyId.
// Returns previous proxy URL strings for cache invalidation.
func ClearProxyFromChannels(proxyId int) ([]string, error) {
	channels, err := channelsBoundToProxyID(proxyId)
	if err != nil {
		return nil, err
	}
	oldURLs := make([]string, 0)
	seen := make(map[string]struct{})
	for i := range channels {
		setting := channels[i].GetSetting()
		old := setting.Proxy
		if old != "" {
			if _, ok := seen[old]; !ok {
				seen[old] = struct{}{}
				oldURLs = append(oldURLs, old)
			}
		}
		setting.ProxyId = 0
		setting.Proxy = ""
		channels[i].SetSetting(setting)
		if err := DB.Model(&channels[i]).Update("setting", channels[i].Setting).Error; err != nil {
			return oldURLs, err
		}
	}
	return oldURLs, nil
}

func attachProxyChannelCounts(list []*Proxy) {
	if len(list) == 0 {
		return
	}
	// One LIKE scan, then count in memory for the page.
	candidates, err := channelsLikelyBoundToProxy()
	if err != nil {
		return
	}
	counts := make(map[int]int64)
	for _, ch := range candidates {
		pid := ch.GetSetting().ProxyId
		if pid > 0 {
			counts[pid]++
		}
	}
	for _, p := range list {
		if p == nil {
			continue
		}
		p.ChannelCount = counts[p.Id]
	}
}

// ResolveProxyFallback walks fallback configuration for an expired proxy.
// Returns:
//   - clear=true, target=nil: rebind channels to direct (no proxy)
//   - clear=false, target!=nil: rebind channels to target
//   - clear=false, target=nil: leave channel bindings unchanged
func ResolveProxyFallback(p *Proxy, now int64, visiting map[int]struct{}) (target *Proxy, clear bool, err error) {
	if p == nil {
		return nil, false, errors.New("proxy is nil")
	}
	if visiting == nil {
		visiting = map[int]struct{}{}
	}
	switch p.FallbackMode {
	case ProxyFallbackDirect:
		return nil, true, nil
	case ProxyFallbackProxy:
		if p.Id > 0 {
			visiting[p.Id] = struct{}{}
		}
		curID := p.BackupProxyId
		for {
			if curID <= 0 {
				return nil, false, nil
			}
			if _, seen := visiting[curID]; seen {
				return nil, false, nil
			}
			visiting[curID] = struct{}{}
			backup, getErr := GetProxyById(curID)
			if getErr != nil {
				return nil, false, nil
			}
			if backup.Status == ProxyStatusActive && !backup.IsExpired(now) {
				return backup, false, nil
			}
			// Backup itself expired/inactive: continue along its chain if configured.
			switch backup.FallbackMode {
			case ProxyFallbackDirect:
				return nil, true, nil
			case ProxyFallbackProxy:
				curID = backup.BackupProxyId
			default:
				return nil, false, nil
			}
		}
	default:
		return nil, false, nil
	}
}

// SweepExpiredProxies marks due active proxies as expired and applies fallback to bound channels.
// Returns the number of channels whose proxy binding was changed.
func SweepExpiredProxies() (int, error) {
	now := common.GetTimestamp()
	var expired []*Proxy
	err := DB.Where("status = ? AND expires_at > 0 AND expires_at < ?", ProxyStatusActive, now).
		Find(&expired).Error
	if err != nil {
		return 0, err
	}

	changedChannels := 0
	for _, p := range expired {
		if p == nil {
			continue
		}
		target, clear, resolveErr := ResolveProxyFallback(p, now, map[int]struct{}{})
		if resolveErr != nil {
			return changedChannels, resolveErr
		}

		bound, listErr := channelsBoundToProxyID(p.Id)
		if listErr != nil {
			return changedChannels, listErr
		}

		if clear {
			if _, clearErr := ClearProxyFromChannels(p.Id); clearErr != nil {
				return changedChannels, clearErr
			}
			changedChannels += len(bound)
		} else if target != nil {
			if _, setErr := SetChannelsProxy(channelIDs(bound), target.Id, target.URL()); setErr != nil {
				return changedChannels, setErr
			}
			changedChannels += len(bound)
		}
		// FallbackMode none or unresolved chain: leave channel bindings.

		p.Status = ProxyStatusExpired
		p.UpdatedTime = now
		if err := DB.Model(p).Select("status", "updated_time").Updates(p).Error; err != nil {
			return changedChannels, err
		}
	}
	return changedChannels, nil
}

func channelIDs(channels []Channel) []int {
	ids := make([]int, 0, len(channels))
	for _, ch := range channels {
		ids = append(ids, ch.Id)
	}
	return ids
}

// StartProxyExpirySweep runs SweepExpiredProxies on a ticker in a background goroutine.
func StartProxyExpirySweep(interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		// Run once shortly after start, then on each tick.
		if n, err := SweepExpiredProxies(); err != nil {
			common.SysError(fmt.Sprintf("[ProxyExpiry] sweep failed: %v", err))
		} else if n > 0 {
			common.SysLog(fmt.Sprintf("[ProxyExpiry] re-routed %d channels off expired proxies", n))
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			n, err := SweepExpiredProxies()
			if err != nil {
				common.SysError(fmt.Sprintf("[ProxyExpiry] sweep failed: %v", err))
				continue
			}
			if n > 0 {
				common.SysLog(fmt.Sprintf("[ProxyExpiry] re-routed %d channels off expired proxies", n))
			}
		}
	}()
}
