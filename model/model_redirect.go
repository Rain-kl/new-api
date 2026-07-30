package model

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ModelRedirect is a virtual model name with an ordered channel+model fallback chain.
// Personal feature — registered via RegisterMainDBModel (low-conflict migrate).
type ModelRedirect struct {
	Id        int                   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name      string                `json:"name" gorm:"size:128;uniqueIndex;not null"` // virtual model name
	Groups    string                `json:"groups" gorm:"type:text"`                   // comma-separated
	Enabled   bool                  `json:"enabled" gorm:"default:true"`
	Remark    string                `json:"remark" gorm:"type:varchar(255);default:''"`
	CreatedAt int64                 `json:"created_at" gorm:"bigint;default:0"`
	UpdatedAt int64                 `json:"updated_at" gorm:"bigint;default:0"`
	Targets   []ModelRedirectTarget `json:"targets,omitempty" gorm:"foreignKey:RedirectId;constraint:OnDelete:CASCADE"`
}

// ModelRedirectTarget is one priority hop: channel + optional upstream model.
// Empty Model means pass through the client virtual model name.
type ModelRedirectTarget struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	RedirectId int    `json:"redirect_id" gorm:"index;not null"`
	Priority   int    `json:"priority" gorm:"not null;default:1"` // lower = higher priority
	ChannelId  int    `json:"channel_id" gorm:"not null;index"`
	Model      string `json:"model" gorm:"size:128;default:''"` // empty = passthrough
	Enabled    bool   `json:"enabled" gorm:"default:true"`
}

// RedirectCandidate is a runtime pick for one attempt.
type RedirectCandidate struct {
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"` // empty = passthrough client model
	Priority  int    `json:"priority"`
}

type modelRedirectCacheEntry struct {
	Groups  map[string]struct{}
	Targets []RedirectCandidate
}

var (
	modelRedirectCacheMu sync.RWMutex
	modelRedirectCache   map[string]*modelRedirectCacheEntry // name -> entry
	modelRedirectLoaded  bool
)

func init() {
	RegisterMainDBModel(&ModelRedirect{})
	RegisterMainDBModel(&ModelRedirectTarget{})
}

// ----- cache -----

func InvalidateModelRedirectCache() {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = nil
	modelRedirectLoaded = false
	modelRedirectCacheMu.Unlock()
}

func buildModelRedirectCacheMap() (map[string]*modelRedirectCacheEntry, error) {
	if DB == nil {
		return map[string]*modelRedirectCacheEntry{}, nil
	}
	var rows []ModelRedirect
	err := DB.Where("enabled = ?", true).Preload("Targets", "enabled = ?", true).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	next := make(map[string]*modelRedirectCacheEntry, len(rows))
	for i := range rows {
		r := &rows[i]
		entry := &modelRedirectCacheEntry{
			Groups:  parseGroupSet(r.Groups),
			Targets: make([]RedirectCandidate, 0, len(r.Targets)),
		}
		targets := append([]ModelRedirectTarget(nil), r.Targets...)
		sort.Slice(targets, func(i, j int) bool {
			return targets[i].Priority < targets[j].Priority
		})
		for _, t := range targets {
			if !t.Enabled || t.ChannelId <= 0 {
				continue
			}
			entry.Targets = append(entry.Targets, RedirectCandidate{
				ChannelID: t.ChannelId,
				Model:     strings.TrimSpace(t.Model),
				Priority:  t.Priority,
			})
		}
		if len(entry.Targets) == 0 {
			continue
		}
		next[strings.TrimSpace(r.Name)] = entry
	}
	return next, nil
}

func LoadModelRedirectCache() error {
	next, err := buildModelRedirectCacheMap()
	if err != nil {
		return err
	}
	modelRedirectCacheMu.Lock()
	modelRedirectCache = next
	modelRedirectLoaded = true
	modelRedirectCacheMu.Unlock()
	return nil
}

func ensureModelRedirectCache() {
	modelRedirectCacheMu.RLock()
	loaded := modelRedirectLoaded
	modelRedirectCacheMu.RUnlock()
	if loaded {
		return
	}
	modelRedirectCacheMu.Lock()
	defer modelRedirectCacheMu.Unlock()
	if modelRedirectLoaded {
		return
	}
	next, err := buildModelRedirectCacheMap()
	if err != nil {
		common.SysLog("load model redirect cache failed: " + err.Error())
		next = map[string]*modelRedirectCacheEntry{}
	}
	modelRedirectCache = next
	modelRedirectLoaded = true
}

func parseGroupSet(groups string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, g := range strings.Split(groups, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			out[g] = struct{}{}
		}
	}
	return out
}

func groupSetContains(set map[string]struct{}, group string) bool {
	if len(set) == 0 {
		return false
	}
	_, ok := set[strings.TrimSpace(group)]
	return ok
}

// ResolveModelRedirect returns ordered candidates when clientModel is a virtual
// model enabled for usingGroup. ok=false means "not a redirect model".
func ResolveModelRedirect(clientModel, usingGroup string) (cands []RedirectCandidate, ok bool) {
	clientModel = strings.TrimSpace(clientModel)
	usingGroup = strings.TrimSpace(usingGroup)
	if clientModel == "" {
		return nil, false
	}
	ensureModelRedirectCache()
	modelRedirectCacheMu.RLock()
	entry := modelRedirectCache[clientModel]
	modelRedirectCacheMu.RUnlock()
	if entry == nil || len(entry.Targets) == 0 {
		return nil, false
	}
	if !groupSetContains(entry.Groups, usingGroup) {
		return nil, false
	}
	out := make([]RedirectCandidate, len(entry.Targets))
	copy(out, entry.Targets)
	return out, true
}

// AttemptModel returns the model name for one hop (passthrough if empty).
func AttemptModel(clientModel string, cand RedirectCandidate) string {
	if strings.TrimSpace(cand.Model) != "" {
		return strings.TrimSpace(cand.Model)
	}
	return strings.TrimSpace(clientModel)
}

// ChannelAccessibleForRedirect reports whether a user in usingGroup may use channel.
// Requires channel enabled and usingGroup listed on the channel.
func ChannelAccessibleForRedirect(channel *Channel, usingGroup string) bool {
	if channel == nil {
		return false
	}
	if channel.Status != common.ChannelStatusEnabled {
		return false
	}
	usingGroup = strings.TrimSpace(usingGroup)
	if usingGroup == "" {
		return false
	}
	for _, g := range channel.GetGroups() {
		if g == usingGroup {
			return true
		}
	}
	return false
}

// FilterRedirectCandidates drops inaccessible / missing channels.
// clientModel is the virtual name (used when target.Model is empty / passthrough).
// pathCheck is optional; when nil, path is not checked.
func FilterRedirectCandidates(
	cands []RedirectCandidate,
	clientModel string,
	usingGroup string,
	requestPath string,
	pathCheck func(ch *Channel, path, model string) bool,
) []RedirectCandidate {
	if len(cands) == 0 {
		return nil
	}
	out := make([]RedirectCandidate, 0, len(cands))
	for _, cand := range cands {
		ch, err := CacheGetChannel(cand.ChannelID)
		if err != nil || ch == nil {
			ch, err = GetChannelById(cand.ChannelID, true)
			if err != nil || ch == nil {
				continue
			}
		}
		if !ChannelAccessibleForRedirect(ch, usingGroup) {
			continue
		}
		attemptModel := AttemptModel(clientModel, cand)
		if pathCheck != nil && !pathCheck(ch, requestPath, attemptModel) {
			continue
		}
		out = append(out, cand)
	}
	return out
}

// GetChannelForRedirect loads channel by id (cache then DB).
func GetChannelForRedirect(channelID int) (*Channel, error) {
	ch, err := CacheGetChannel(channelID)
	if err == nil && ch != nil {
		return ch, nil
	}
	return GetChannelById(channelID, true)
}

// ----- CRUD -----

type ModelRedirectInput struct {
	Name    string                     `json:"name"`
	Groups  []string                   `json:"groups"`
	Enabled *bool                      `json:"enabled"`
	Remark  string                     `json:"remark"`
	Targets []ModelRedirectTargetInput `json:"targets"`
}

type ModelRedirectTargetInput struct {
	Priority  int    `json:"priority"`
	ChannelId int    `json:"channel_id"`
	Model     string `json:"model"`
	Enabled   *bool  `json:"enabled"`
}

func normalizeGroupList(groups []string) string {
	seen := make(map[string]struct{})
	var parts []string
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		parts = append(parts, g)
	}
	return strings.Join(parts, ",")
}

func validateModelRedirectInput(in *ModelRedirectInput, isCreate bool) error {
	if in == nil {
		return fmt.Errorf("invalid request")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return fmt.Errorf("model name is required")
	}
	if len(name) > 128 {
		return fmt.Errorf("model name too long")
	}
	groups := normalizeGroupList(in.Groups)
	if groups == "" {
		return fmt.Errorf("at least one group is required")
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("at least one redirect target is required")
	}
	prioSeen := make(map[int]struct{})
	for i, t := range in.Targets {
		if t.ChannelId <= 0 {
			return fmt.Errorf("target[%d]: channel_id is required", i)
		}
		if _, err := GetChannelById(t.ChannelId, false); err != nil {
			return fmt.Errorf("target[%d]: channel %d not found", i, t.ChannelId)
		}
		p := t.Priority
		if p <= 0 {
			p = i + 1
		}
		if _, ok := prioSeen[p]; ok {
			return fmt.Errorf("duplicate priority %d", p)
		}
		prioSeen[p] = struct{}{}
	}
	if isCreate {
		var count int64
		if err := DB.Model(&ModelRedirect{}).Where("name = ?", name).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("model name already exists")
		}
	}
	return nil
}

func GetAllModelRedirects() ([]*ModelRedirect, error) {
	var rows []*ModelRedirect
	err := DB.Preload("Targets").Order("id desc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r == nil {
			continue
		}
		sort.Slice(r.Targets, func(i, j int) bool {
			return r.Targets[i].Priority < r.Targets[j].Priority
		})
	}
	return rows, nil
}

func GetModelRedirectById(id int) (*ModelRedirect, error) {
	var row ModelRedirect
	err := DB.Preload("Targets").First(&row, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	sort.Slice(row.Targets, func(i, j int) bool {
		return row.Targets[i].Priority < row.Targets[j].Priority
	})
	return &row, nil
}

func CreateModelRedirect(in *ModelRedirectInput) (*ModelRedirect, error) {
	if err := validateModelRedirectInput(in, true); err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	row := &ModelRedirect{
		Name:      strings.TrimSpace(in.Name),
		Groups:    normalizeGroupList(in.Groups),
		Enabled:   enabled,
		Remark:    in.Remark,
		CreatedAt: now,
		UpdatedAt: now,
	}
	for i, t := range in.Targets {
		p := t.Priority
		if p <= 0 {
			p = i + 1
		}
		te := true
		if t.Enabled != nil {
			te = *t.Enabled
		}
		row.Targets = append(row.Targets, ModelRedirectTarget{
			Priority:  p,
			ChannelId: t.ChannelId,
			Model:     strings.TrimSpace(t.Model),
			Enabled:   te,
		})
	}
	if err := DB.Create(row).Error; err != nil {
		return nil, err
	}
	InvalidateModelRedirectCache()
	return GetModelRedirectById(row.Id)
}

func UpdateModelRedirect(id int, in *ModelRedirectInput) (*ModelRedirect, error) {
	if err := validateModelRedirectInput(in, false); err != nil {
		return nil, err
	}
	existing, err := GetModelRedirectById(id)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name != existing.Name {
		var count int64
		if err := DB.Model(&ModelRedirect{}).Where("name = ? AND id <> ?", name, id).Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, fmt.Errorf("model name already exists")
		}
	}
	enabled := existing.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	if err := tx.Model(&ModelRedirect{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":       name,
		"groups":     normalizeGroupList(in.Groups),
		"enabled":    enabled,
		"remark":     in.Remark,
		"updated_at": common.GetTimestamp(),
	}).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Where("redirect_id = ?", id).Delete(&ModelRedirectTarget{}).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	for i, t := range in.Targets {
		p := t.Priority
		if p <= 0 {
			p = i + 1
		}
		te := true
		if t.Enabled != nil {
			te = *t.Enabled
		}
		target := ModelRedirectTarget{
			RedirectId: id,
			Priority:   p,
			ChannelId:  t.ChannelId,
			Model:      strings.TrimSpace(t.Model),
			Enabled:    te,
		}
		if err := tx.Create(&target).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	InvalidateModelRedirectCache()
	return GetModelRedirectById(id)
}

func DeleteModelRedirect(id int) error {
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Where("redirect_id = ?", id).Delete(&ModelRedirectTarget{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Delete(&ModelRedirect{}, "id = ?", id).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	InvalidateModelRedirectCache()
	return nil
}

func UpdateModelRedirectStatus(id int, enabled bool) error {
	err := DB.Model(&ModelRedirect{}).Where("id = ?", id).Updates(map[string]interface{}{
		"enabled":    enabled,
		"updated_at": common.GetTimestamp(),
	}).Error
	if err != nil {
		return err
	}
	InvalidateModelRedirectCache()
	return nil
}

// GetEnabledModelRedirectNamesForGroup returns virtual model names usable by group.
func GetEnabledModelRedirectNamesForGroup(group string) []string {
	ensureModelRedirectCache()
	group = strings.TrimSpace(group)
	modelRedirectCacheMu.RLock()
	defer modelRedirectCacheMu.RUnlock()
	var names []string
	for name, entry := range modelRedirectCache {
		if groupSetContains(entry.Groups, group) {
			names = append(names, name)
		}
	}
	return names
}
