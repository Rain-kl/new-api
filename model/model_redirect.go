package model

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

// ModelRedirect is a virtual model name with an ordered channel+model fallback chain.
// Personal feature — registered via RegisterMainDBModel (low-conflict migrate).
type ModelRedirect struct {
	Id     int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name   string `json:"name" gorm:"size:128;uniqueIndex;not null"` // virtual model name
	Groups string `json:"groups" gorm:"type:text"`                   // comma-separated
	// No gorm default tag on Enabled: the `default:true` tag makes GORM omit the
	// false zero value on Create (stored as enabled). The true default is enforced
	// in CreateModelRedirect / UpdateModelRedirect, so the tag is unnecessary.
	Enabled       bool                  `json:"enabled"`
	Remark        string                `json:"remark" gorm:"type:varchar(255);default:''"`
	Mode          string                `json:"mode" gorm:"size:16;default:redirect"`      // "redirect" | "mapping"
	MappingTarget string                `json:"mapping_target" gorm:"size:128;default:''"` // mapping-mode target model name
	CreatedAt     int64                 `json:"created_at" gorm:"bigint;default:0"`
	UpdatedAt     int64                 `json:"updated_at" gorm:"bigint;default:0"`
	Targets       []ModelRedirectTarget `json:"targets,omitempty" gorm:"foreignKey:RedirectId;constraint:OnDelete:CASCADE"`
}

// ModelRedirectTarget is one priority hop: channel + optional upstream model.
// Empty Model means pass through the client virtual model name.
//
// Priority: larger number = higher priority. Targets with the same Priority form
// a load-balancing pool (currently equal share; Weight is reserved for future
// weighted balancing and is not applied yet).
type ModelRedirectTarget struct {
	Id         int `json:"id" gorm:"primaryKey;autoIncrement"`
	RedirectId int `json:"redirect_id" gorm:"index;not null"`
	Priority   int `json:"priority" gorm:"not null;default:100"` // higher = preferred
	// Weight is reserved for future weighted LB among same-priority targets.
	// 0 means "equal share" (current behaviour). Non-zero values are stored but
	// not yet applied at resolve time.
	Weight    int    `json:"weight" gorm:"not null;default:0"`
	ChannelId int    `json:"channel_id" gorm:"not null;index"`
	Model     string `json:"model" gorm:"size:128;default:''"` // empty = passthrough
	// No gorm default tag on Enabled: the `default:true` tag makes GORM omit the
	// false zero value on Create (stored as enabled). The true default is enforced
	// in buildTargetsFromInput, so the tag is both unnecessary and harmful.
	Enabled bool `json:"enabled"`
}

// RedirectCandidate is a runtime pick for one attempt.
type RedirectCandidate struct {
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"` // empty = passthrough client model
	Priority  int    `json:"priority"`
	Weight    int    `json:"weight"` // reserved; 0 = equal share among same priority
}

func (c RedirectCandidate) IsNestedRedirect() bool {
	return c.ChannelID == constant.ModelRedirectSentinelChannelID
}

// IsModelOnly reports whether the hop carries only a model name and no concrete
// channel (ChannelID == 0); the channel layer selects a channel at pick time.
func (c RedirectCandidate) IsModelOnly() bool {
	return c.ChannelID == 0 && strings.TrimSpace(c.Model) != ""
}

type modelRedirectCacheEntry struct {
	Mode          string
	MappingTarget string
	Groups        map[string]struct{}
	Targets       []RedirectCandidate
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

const (
	modelRedirectMaxPriority           = 1_000_000
	modelRedirectMaxTargets            = 64
	modelRedirectMaxModelLen           = 128
	modelRedirectMaxRemarkLen          = 255
	modelRedirectDefaultPrio           = 100
	modelRedirectMaxExpandDepth        = 32
	modelRedirectMaxExpandedCandidates = 128
)

const (
	ModelRedirectModeRedirect = "redirect"
	ModelRedirectModeMapping  = "mapping"
)

func InvalidateModelRedirectCache() {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = nil
	modelRedirectLoaded = false
	modelRedirectCacheMu.Unlock()
	// Model plaza /api/pricing is cached for ~1min; drop it when redirects change.
	InvalidatePricingCache()
}

func sortModelRedirectTargets(targets []ModelRedirectTarget) {
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Priority != targets[j].Priority {
			return targets[i].Priority > targets[j].Priority
		}
		return targets[i].Id < targets[j].Id
	})
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
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		groups := parseGroupSet(r.Groups)
		if len(groups) == 0 {
			continue
		}
		mode := strings.TrimSpace(r.Mode)
		if mode == "" {
			mode = ModelRedirectModeRedirect
		}
		entry := &modelRedirectCacheEntry{
			Mode:          mode,
			MappingTarget: strings.TrimSpace(r.MappingTarget),
			Groups:        groups,
		}
		if mode == ModelRedirectModeMapping {
			if entry.MappingTarget == "" {
				continue
			}
			next[name] = entry
			continue
		}
		targets := append([]ModelRedirectTarget(nil), r.Targets...)
		// Cache keeps higher priority first; same-priority order is rebalanced at resolve.
		sortModelRedirectTargets(targets)
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
		if len(entry.Targets) == 0 {
			continue
		}
		next[name] = entry
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
		// Do not mark loaded: allow the next request to retry after a transient DB error.
		common.SysLog("load model redirect cache failed: " + err.Error())
		if modelRedirectCache == nil {
			modelRedirectCache = map[string]*modelRedirectCacheEntry{}
		}
		return
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

// resolveModelRedirectEntry recursively resolves a virtual model into terminal
// candidates (channel-bound or model-only). priorityOverride threads the priority
// of the referencing redirect target through mapping chains (0 = none / top-level).
// Terminates at a channel hop (redirect mode) or at a model-only candidate when
// the mapping target is not itself a virtual model.
// Caller must not hold modelRedirectCacheMu; this function takes RLock as needed.
func resolveModelRedirectEntry(name, usingGroup string, depth int, stack map[string]struct{}, priorityOverride int) []RedirectCandidate {
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

	modelRedirectCacheMu.RLock()
	entry := modelRedirectCache[name]
	var found bool
	var mode string
	var mappingTarget string
	var targets []RedirectCandidate
	if entry != nil && groupSetContains(entry.Groups, usingGroup) {
		found = true
		mode = entry.Mode
		mappingTarget = strings.TrimSpace(entry.MappingTarget)
		targets = make([]RedirectCandidate, len(entry.Targets))
		copy(targets, entry.Targets)
	}
	modelRedirectCacheMu.RUnlock()
	if !found {
		return nil
	}
	stack[name] = struct{}{}
	defer delete(stack, name)

	if mode == ModelRedirectModeMapping {
		if mappingTarget == "" {
			return nil
		}
		// Re-check the target against the virtual list; keep resolving if it is one.
		modelRedirectCacheMu.RLock()
		_, isVirtual := modelRedirectCache[mappingTarget]
		modelRedirectCacheMu.RUnlock()
		if isVirtual {
			return resolveModelRedirectEntry(mappingTarget, usingGroup, depth+1, stack, priorityOverride)
		}
		p := priorityOverride
		if p <= 0 {
			p = modelRedirectDefaultPrio
		}
		return []RedirectCandidate{{ChannelID: 0, Model: mappingTarget, Priority: p}}
	}

	// redirect mode (mode may be "" in entries built before the field existed)
	out := make([]RedirectCandidate, 0, len(targets))
	for _, t := range targets {
		if t.IsNestedRedirect() {
			child := strings.TrimSpace(t.Model)
			if child == "" {
				continue
			}
			nested := resolveModelRedirectEntry(child, usingGroup, depth+1, stack, t.Priority)
			out = append(out, nested...)
		} else if t.ChannelID > 0 {
			cand := t
			if strings.TrimSpace(cand.Model) == "" {
				cand.Model = name // owning virtual model of this cache entry
			}
			out = append(out, cand)
		}
		if len(out) >= modelRedirectMaxExpandedCandidates {
			common.SysLog(fmt.Sprintf("model redirect expand truncated at %d for %q", modelRedirectMaxExpandedCandidates, name))
			return out[:modelRedirectMaxExpandedCandidates]
		}
	}
	return out
}

// ResolveModelRedirect returns a defensive copy of terminal candidates when
// clientModel is an enabled virtual model for usingGroup. Mapping entries
// resolve through their target (recursively); redirect entries expand their
// priority chain. ok=false means "not a redirect/mapping model" or resolution
// yielded no usable hops.
//
// Order is priority DESC within each redirect's cache entry (stable). Call
// OrderRedirectCandidates after filtering inaccessible channels so same-priority
// load balancing only covers live peers.
func ResolveModelRedirect(clientModel, usingGroup string) (cands []RedirectCandidate, ok bool) {
	clientModel = strings.TrimSpace(clientModel)
	usingGroup = strings.TrimSpace(usingGroup)
	if clientModel == "" || usingGroup == "" {
		return nil, false
	}
	ensureModelRedirectCache()
	stack := map[string]struct{}{}
	out := resolveModelRedirectEntry(clientModel, usingGroup, 0, stack, 0)
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// OrderRedirectCandidates preserves input candidate order (e.g. expand black-box
// order) and load-balances only within contiguous runs of equal Priority
// (equal-share shuffle). Weight is stored for forward-compat but not applied yet.
// Prefer calling this after FilterRedirectCandidates so LB only covers usable hops.
func OrderRedirectCandidates(cands []RedirectCandidate) []RedirectCandidate {
	return orderRedirectCandidates(cands)
}

// OrderRedirectCandidatesWithAffinity promotes the bound channel to the front of
// each contiguous equal-priority run that contains it; runs without it keep the
// existing equal-share shuffle. preferredChannelID <= 0 behaves exactly like
// OrderRedirectCandidates. Promotion is stable: the preferred channel moves to
// the front of its run and the other members keep relative order (no re-shuffle).
func OrderRedirectCandidatesWithAffinity(cands []RedirectCandidate, preferredChannelID int) []RedirectCandidate {
	if preferredChannelID <= 0 {
		return orderRedirectCandidates(cands)
	}
	if len(cands) == 0 {
		return nil
	}
	// Work on a copy so callers can safely pass cache-backed or shared slices.
	work := make([]RedirectCandidate, len(cands))
	copy(work, cands)

	// Same run-boundary walk as orderRedirectCandidates; promote within the run.
	i := 0
	for i < len(work) {
		j := i + 1
		for j < len(work) && work[j].Priority == work[i].Priority {
			j++
		}
		run := work[i:j]
		idx := -1
		for k := range run {
			if run[k].ChannelID == preferredChannelID {
				idx = k
				break
			}
		}
		if idx >= 0 {
			preferred := run[idx]
			copy(run[1:idx+1], run[0:idx])
			run[0] = preferred
		} else {
			shuffleRedirectCandidatesEqual(run)
		}
		i = j
	}
	return work
}

// orderRedirectCandidates preserves input order and shuffles only contiguous
// equal-Priority runs. It does not regroup non-contiguous same priorities or
// re-sort by priority globally (required so nested expand black-box order sticks).
// Weight is accepted for forward-compat but equal share is used until weighted LB.
func orderRedirectCandidates(cands []RedirectCandidate) []RedirectCandidate {
	if len(cands) == 0 {
		return nil
	}
	if len(cands) == 1 {
		return []RedirectCandidate{cands[0]}
	}
	// Work on a copy so callers can safely pass cache-backed or shared slices.
	work := make([]RedirectCandidate, len(cands))
	copy(work, cands)

	// Shuffle each contiguous equal-priority run in place; leave run boundaries fixed.
	i := 0
	for i < len(work) {
		j := i + 1
		for j < len(work) && work[j].Priority == work[i].Priority {
			j++
		}
		// Equal-share shuffle. Future: weighted pick using work[i:j].Weight.
		shuffleRedirectCandidatesEqual(work[i:j])
		i = j
	}
	return work
}

func shuffleRedirectCandidatesEqual(cands []RedirectCandidate) {
	// Fisher–Yates; rand/v2 is concurrency-safe.
	for i := len(cands) - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		cands[i], cands[j] = cands[j], cands[i]
	}
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
		if cand.IsModelOnly() {
			// No concrete channel yet; path/model availability is checked when the
			// channel layer selects a channel for cand.Model at pick time.
			out = append(out, cand)
			continue
		}
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

// FirstUsableRedirectCandidate returns the first candidate that yields a usable
// channel (accessible, not cooled) together with that channel and the attempt
// model. Model-only candidates walk channel-priority tiers (0 .. modelSlots-1),
// mirroring the retry-path slot resolution, so a cooled top-priority channel
// falls through to the next tier instead of skipping the whole model. Returns
// (nil, "", -1) when no candidate is usable.
func FirstUsableRedirectCandidate(cands []RedirectCandidate, clientModel, group, requestPath string, modelSlots int) (*Channel, string, int) {
	if modelSlots < 1 {
		modelSlots = 1
	}
	for i, cand := range cands {
		if cand.ChannelID > 0 {
			ch, err := GetChannelForRedirect(cand.ChannelID)
			if err != nil || ch == nil {
				continue
			}
			if IsModelRedirectHopDisabled(cand.ChannelID, AttemptModel(clientModel, cand)) {
				continue
			}
			return ch, AttemptModel(clientModel, cand), i
		}
		if cand.IsModelOnly() {
			for level := 0; level < modelSlots; level++ {
				ch, _ := GetRandomSatisfiedChannel(group, cand.Model, level, requestPathFilters(requestPath))
				if ch == nil {
					break
				}
				if IsModelRedirectHopDisabled(ch.Id, cand.Model) {
					continue
				}
				return ch, cand.Model, i
			}
		}
	}
	return nil, "", -1
}

// redirectSlotMapping maps a 0-based attempt slot to (candidate index, level).
// Channel-bound candidates span 1 slot; model-only candidates span modelSlots
// slots (one per channel-priority tier). Returns ok=false when slot is beyond
// the candidate list.
func redirectSlotMapping(cands []RedirectCandidate, slot, modelSlots int) (candIdx, level int, ok bool) {
	if len(cands) == 0 || slot < 0 || modelSlots < 1 {
		return 0, 0, false
	}
	rangeStart := 0
	for i, cand := range cands {
		span := 1
		if cand.IsModelOnly() {
			span = modelSlots
		}
		rangeEnd := rangeStart + span
		if slot < rangeEnd {
			return i, slot - rangeStart, true
		}
		rangeStart = rangeEnd
	}
	return 0, 0, false
}

// RedirectSlotCount returns the total number of retry slots a candidate list
// spans: channel-bound candidates occupy 1 slot each, model-only candidates
// occupy modelSlots slots each (one per channel-priority tier).
func RedirectSlotCount(cands []RedirectCandidate, modelSlots int) int {
	if modelSlots < 1 {
		modelSlots = 1
	}
	total := 0
	for _, cand := range cands {
		if cand.IsModelOnly() {
			total += modelSlots
		} else {
			total++
		}
	}
	return total
}

// ResolveRedirectSlot resolves the first usable slot at or after slot, returning
// the channel, the attempt model, and the actual slot index used. Slots whose
// candidate has no usable channel are skipped, so an exhausted hop falls through
// to the next redirect candidate instead of aborting the whole request. Returns
// (nil, "", slot) when every remaining slot is unusable.
func ResolveRedirectSlot(cands []RedirectCandidate, slot int, clientModel, group, requestPath string, modelSlots int) (*Channel, string, int) {
	total := RedirectSlotCount(cands, modelSlots)
	for s := slot; s < total; s++ {
		channel, attemptModel := resolveRedirectSlotAt(cands, s, clientModel, group, requestPath, modelSlots)
		if channel != nil {
			return channel, attemptModel, s
		}
	}
	return nil, "", slot
}

// resolveRedirectSlotAt resolves a single slot to a channel + attempt model, or
// (nil, "") when the slot's candidate has no usable channel.
func resolveRedirectSlotAt(cands []RedirectCandidate, slot int, clientModel, group, requestPath string, modelSlots int) (*Channel, string) {
	candIdx, level, ok := redirectSlotMapping(cands, slot, modelSlots)
	if !ok {
		return nil, ""
	}
	cand := cands[candIdx]
	if cand.ChannelID > 0 {
		ch, err := GetChannelForRedirect(cand.ChannelID)
		if err != nil || ch == nil {
			return nil, ""
		}
		return ch, AttemptModel(clientModel, cand)
	}
	if cand.IsModelOnly() {
		ch, _ := GetRandomSatisfiedChannel(group, cand.Model, level, requestPathFilters(requestPath))
		if ch == nil {
			return nil, ""
		}
		return ch, cand.Model
	}
	return nil, ""
}

func requestPathFilters(requestPath string) []dto.ChannelFilter {
	if requestPath == "" {
		return nil
	}
	return []dto.ChannelFilter{
		{
			Kind:        dto.FilterRequestPath,
			RequestPath: requestPath,
		},
	}
}

// ----- CRUD -----

type ModelRedirectInput struct {
	Name          string                     `json:"name"`
	Groups        []string                   `json:"groups"`
	Enabled       *bool                      `json:"enabled"`
	Remark        string                     `json:"remark"`
	Mode          string                     `json:"mode"`
	MappingTarget string                     `json:"mapping_target"`
	Targets       []ModelRedirectTargetInput `json:"targets"`
}

type ModelRedirectTargetInput struct {
	Priority int `json:"priority"`
	// Weight is optional/reserved for future weighted LB among same priority.
	// 0 or omitted = equal share (current behaviour).
	Weight    *int   `json:"weight,omitempty"`
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

// detectModelRedirectCycle reports whether start can reach itself via nested
// redirect edges (DFS with path set). Missing keys are treated as no edges.
func detectModelRedirectCycle(start string, edges map[string][]string) bool {
	start = strings.TrimSpace(start)
	if start == "" {
		return false
	}
	path := make(map[string]struct{})
	var visit func(node string) bool
	visit = func(node string) bool {
		node = strings.TrimSpace(node)
		if node == "" {
			return false
		}
		if _, onPath := path[node]; onPath {
			return true
		}
		path[node] = struct{}{}
		for _, next := range edges[node] {
			if visit(next) {
				return true
			}
		}
		delete(path, node)
		return false
	}
	return visit(start)
}

// buildModelRedirectNestedEdgeMap builds name -> child names from rows. Edges
// come from redirect sentinel targets and from mapping-mode mapping targets.
// Only enabled rows contribute edges.
func buildModelRedirectNestedEdgeMap(rows []*ModelRedirect) map[string][]string {
	edges := make(map[string][]string, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		var children []string
		if strings.TrimSpace(r.Mode) == ModelRedirectModeMapping {
			if t := strings.TrimSpace(r.MappingTarget); t != "" {
				children = append(children, t)
			}
			edges[name] = children
			continue
		}
		for _, t := range r.Targets {
			if !t.Enabled {
				continue
			}
			if t.ChannelId != constant.ModelRedirectSentinelChannelID {
				continue
			}
			child := strings.TrimSpace(t.Model)
			if child == "" {
				continue
			}
			children = append(children, child)
		}
		edges[name] = children
	}
	return edges
}

// nestedEdgesFromInput returns the save-payload's edges for cycle detection:
// the mapping target for mapping-mode input, or enabled nested-ref child names
// for redirect-mode input.
func nestedEdgesFromInput(in *ModelRedirectInput) []string {
	if strings.TrimSpace(in.Mode) == ModelRedirectModeMapping {
		if t := strings.TrimSpace(in.MappingTarget); t != "" {
			return []string{t}
		}
		return nil
	}
	var children []string
	for _, t := range in.Targets {
		enabled := true
		if t.Enabled != nil {
			enabled = *t.Enabled
		}
		if !enabled {
			continue
		}
		if t.ChannelId != constant.ModelRedirectSentinelChannelID {
			continue
		}
		child := strings.TrimSpace(t.Model)
		if child == "" {
			continue
		}
		children = append(children, child)
	}
	return children
}

func validateModelRedirectInput(in *ModelRedirectInput, isCreate bool, selfName string) error {
	if in == nil {
		return fmt.Errorf("invalid request")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return fmt.Errorf("model name is required")
	}
	if len(name) > modelRedirectMaxModelLen {
		return fmt.Errorf("model name too long")
	}
	if strings.ContainsAny(name, ",\n\r\t") {
		return fmt.Errorf("model name must not contain commas or whitespace control characters")
	}
	groups := normalizeGroupList(in.Groups)
	if groups == "" {
		return fmt.Errorf("at least one group is required")
	}
	if len(in.Remark) > modelRedirectMaxRemarkLen {
		return fmt.Errorf("remark too long (max %d)", modelRedirectMaxRemarkLen)
	}
	mode := strings.TrimSpace(in.Mode)
	if mode == "" {
		mode = ModelRedirectModeRedirect
	}
	if mode != ModelRedirectModeRedirect && mode != ModelRedirectModeMapping {
		return fmt.Errorf("invalid mode %q", mode)
	}
	mappingTarget := strings.TrimSpace(in.MappingTarget)
	if mode == ModelRedirectModeMapping {
		if mappingTarget == "" {
			return fmt.Errorf("mapping target is required in mapping mode")
		}
		if len(mappingTarget) > modelRedirectMaxModelLen {
			return fmt.Errorf("mapping target too long")
		}
		if strings.ContainsAny(mappingTarget, ",\n\r\t") {
			return fmt.Errorf("mapping target must not contain commas or whitespace control characters")
		}
		// A mapping target that names an existing virtual model must be enabled.
		if DB != nil {
			var exists int64
			if err := DB.Model(&ModelRedirect{}).Where("name = ?", mappingTarget).Count(&exists).Error; err != nil {
				return err
			}
			if exists > 0 {
				var enabledCount int64
				if err := DB.Model(&ModelRedirect{}).Where("name = ? AND enabled = ?", mappingTarget, true).Count(&enabledCount).Error; err != nil {
					return err
				}
				if enabledCount == 0 {
					return fmt.Errorf("mapping target %q is a virtual model but disabled", mappingTarget)
				}
			}
		}
	} else {
		if len(in.Targets) == 0 {
			return fmt.Errorf("at least one redirect target is required")
		}
		if len(in.Targets) > modelRedirectMaxTargets {
			return fmt.Errorf("too many targets (max %d)", modelRedirectMaxTargets)
		}

		hasEnabled := false
		// Same Priority is allowed (equal load balancing among that tier).
		for i, t := range in.Targets {
			modelName := strings.TrimSpace(t.Model)
			switch {
			case t.ChannelId == constant.ModelRedirectSentinelChannelID:
				// Nested virtual-model ref: model required, no self-ref, child must be enabled redirect.
				if modelName == "" {
					return fmt.Errorf("target[%d]: nested model is required", i)
				}
				if len(modelName) > modelRedirectMaxModelLen {
					return fmt.Errorf("target[%d]: model name too long", i)
				}
				if modelName == name {
					return fmt.Errorf("target[%d]: nested model must not reference itself", i)
				}
				if DB != nil {
					var n int64
					err := DB.Model(&ModelRedirect{}).Where("name = ? AND enabled = ?", modelName, true).Count(&n).Error
					if err != nil {
						return err
					}
					if n == 0 {
						return fmt.Errorf("target[%d]: nested model %q not found or not enabled", i, modelName)
					}
				}
			case t.ChannelId > 0:
				if _, err := GetChannelById(t.ChannelId, false); err != nil {
					return fmt.Errorf("target[%d]: channel %d not found", i, t.ChannelId)
				}
				if len(modelName) > modelRedirectMaxModelLen {
					return fmt.Errorf("target[%d]: model name too long", i)
				}
			default:
				return fmt.Errorf("target[%d]: invalid channel_id", i)
			}
			if t.Priority > modelRedirectMaxPriority {
				return fmt.Errorf("target[%d]: priority too large (max %d)", i, modelRedirectMaxPriority)
			}
			if t.Weight != nil && *t.Weight < 0 {
				return fmt.Errorf("target[%d]: weight must be >= 0", i)
			}
			enabled := true
			if t.Enabled != nil {
				enabled = *t.Enabled
			}
			if enabled {
				hasEnabled = true
			}
		}
		if !hasEnabled {
			return fmt.Errorf("at least one enabled target is required")
		}
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

	// Cycle detection: substitute this node's nested edges with the input, then DFS.
	if DB != nil {
		var rows []*ModelRedirect
		if err := DB.Preload("Targets").Find(&rows).Error; err != nil {
			return err
		}
		edges := buildModelRedirectNestedEdgeMap(rows)
		selfName = strings.TrimSpace(selfName)
		if selfName != "" && selfName != name {
			// Rename: drop old name so it does not leave a phantom node in the graph.
			delete(edges, selfName)
		}
		edges[name] = nestedEdgesFromInput(in)
		if detectModelRedirectCycle(name, edges) {
			return fmt.Errorf("model redirect cycle detected")
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
		sortModelRedirectTargets(r.Targets)
	}
	return rows, nil
}

func GetModelRedirectById(id int) (*ModelRedirect, error) {
	var row ModelRedirect
	err := DB.Preload("Targets").First(&row, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	sortModelRedirectTargets(row.Targets)
	return &row, nil
}

func normalizeTargetPriority(p int, index int) int {
	if p > 0 {
		if p > modelRedirectMaxPriority {
			return modelRedirectMaxPriority
		}
		return p
	}
	// Fallback when client omits priority: descending-friendly defaults, always >= 1.
	v := modelRedirectDefaultPrio - index
	if v < 1 {
		return 1
	}
	return v
}

func normalizeTargetWeight(w *int) int {
	if w == nil || *w < 0 {
		return 0
	}
	return *w
}

func normalizeRemark(remark string) string {
	if len(remark) > modelRedirectMaxRemarkLen {
		return remark[:modelRedirectMaxRemarkLen]
	}
	return remark
}

// buildTargetsFromInput normalizes and de-duplicates targets
// (same priority + channel + model + enabled).
func buildTargetsFromInput(inTargets []ModelRedirectTargetInput, redirectId int) []ModelRedirectTarget {
	out := make([]ModelRedirectTarget, 0, len(inTargets))
	seen := make(map[string]struct{}, len(inTargets))
	for i, t := range inTargets {
		te := true
		if t.Enabled != nil {
			te = *t.Enabled
		}
		modelName := strings.TrimSpace(t.Model)
		p := normalizeTargetPriority(t.Priority, i)
		key := fmt.Sprintf("%d|%d|%s|%t", p, t.ChannelId, modelName, te)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ModelRedirectTarget{
			RedirectId: redirectId,
			Priority:   p,
			Weight:     normalizeTargetWeight(t.Weight),
			ChannelId:  t.ChannelId,
			Model:      modelName,
			Enabled:    te,
		})
	}
	return out
}

func CreateModelRedirect(in *ModelRedirectInput) (*ModelRedirect, error) {
	if err := validateModelRedirectInput(in, true, ""); err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	mode := strings.TrimSpace(in.Mode)
	if mode == "" {
		mode = ModelRedirectModeRedirect
	}
	row := &ModelRedirect{
		Name:          strings.TrimSpace(in.Name),
		Groups:        normalizeGroupList(in.Groups),
		Enabled:       enabled,
		Mode:          mode,
		MappingTarget: strings.TrimSpace(in.MappingTarget),
		Remark:        normalizeRemark(in.Remark),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if mode == ModelRedirectModeRedirect {
		row.Targets = buildTargetsFromInput(in.Targets, 0)
		if len(row.Targets) == 0 {
			return nil, fmt.Errorf("at least one redirect target is required")
		}
	}
	if err := DB.Create(row).Error; err != nil {
		return nil, err
	}
	InvalidateModelRedirectCache()
	return GetModelRedirectById(row.Id)
}

func UpdateModelRedirect(id int, in *ModelRedirectInput) (*ModelRedirect, error) {
	existing, err := GetModelRedirectById(id)
	if err != nil {
		return nil, err
	}
	if err := validateModelRedirectInput(in, false, existing.Name); err != nil {
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
	mode := strings.TrimSpace(in.Mode)
	if mode == "" {
		mode = ModelRedirectModeRedirect
	}
	updates := map[string]interface{}{
		"name":       name,
		"groups":     normalizeGroupList(in.Groups),
		"enabled":    enabled,
		"mode":       mode,
		"remark":     normalizeRemark(in.Remark),
		"updated_at": common.GetTimestamp(),
	}
	if mode == ModelRedirectModeMapping {
		// Write the active mapping target; redirect targets stay untouched.
		updates["mapping_target"] = strings.TrimSpace(in.MappingTarget)
	} else {
		// Redirect mode never touches the stored mapping target (non-destructive).
		updates["mapping_target"] = existing.MappingTarget
	}

	tx := DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	if err := tx.Model(&ModelRedirect{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if mode == ModelRedirectModeRedirect {
		targets := buildTargetsFromInput(in.Targets, id)
		if len(targets) == 0 {
			tx.Rollback()
			return nil, fmt.Errorf("at least one redirect target is required")
		}
		if err := tx.Where("redirect_id = ?", id).Delete(&ModelRedirectTarget{}).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		for i := range targets {
			if err := tx.Create(&targets[i]).Error; err != nil {
				tx.Rollback()
				return nil, err
			}
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

// IsModelRedirectVirtual reports whether name is an enabled virtual model redirect.
func IsModelRedirectVirtual(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	ensureModelRedirectCache()
	modelRedirectCacheMu.RLock()
	_, ok := modelRedirectCache[name]
	modelRedirectCacheMu.RUnlock()
	return ok
}

// ModelRedirectEnableGroups returns groups that can use the virtual model.
func ModelRedirectEnableGroups(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	ensureModelRedirectCache()
	modelRedirectCacheMu.RLock()
	entry := modelRedirectCache[name]
	modelRedirectCacheMu.RUnlock()
	if entry == nil {
		return nil
	}
	out := make([]string, 0, len(entry.Groups))
	for g := range entry.Groups {
		out = append(out, g)
	}
	return out
}

// ModelRedirectDisplaySourceModel returns the model used for pricing/display when
// the virtual name itself has no ratio/price: the mapping target for mapping
// entries, or the first (highest-priority) target model for redirect entries.
func ModelRedirectDisplaySourceModel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	ensureModelRedirectCache()
	modelRedirectCacheMu.RLock()
	entry := modelRedirectCache[name]
	if entry == nil {
		modelRedirectCacheMu.RUnlock()
		return name
	}
	if entry.Mode == ModelRedirectModeMapping {
		target := strings.TrimSpace(entry.MappingTarget)
		modelRedirectCacheMu.RUnlock()
		if target == "" {
			return name
		}
		return target
	}
	if len(entry.Targets) == 0 {
		modelRedirectCacheMu.RUnlock()
		return name
	}
	first := entry.Targets[0]
	modelRedirectCacheMu.RUnlock()
	return AttemptModel(name, first)
}

// ForEachEnabledModelRedirect walks enabled virtual models (name + groups +
// display source). Callback runs without holding the cache lock so callers may
// do heavier work safely.
func ForEachEnabledModelRedirect(fn func(name string, groups []string, displaySource string)) {
	if fn == nil {
		return
	}
	ensureModelRedirectCache()
	modelRedirectCacheMu.RLock()
	type snap struct {
		name          string
		groups        []string
		displaySource string
	}
	items := make([]snap, 0, len(modelRedirectCache))
	for name, entry := range modelRedirectCache {
		if entry == nil {
			continue
		}
		groups := make([]string, 0, len(entry.Groups))
		for g := range entry.Groups {
			groups = append(groups, g)
		}
		displaySource := name
		if entry.Mode == ModelRedirectModeMapping {
			if target := strings.TrimSpace(entry.MappingTarget); target != "" {
				displaySource = target
			}
		} else if len(entry.Targets) > 0 {
			displaySource = AttemptModel(name, entry.Targets[0])
		}
		items = append(items, snap{
			name:          name,
			groups:        groups,
			displaySource: displaySource,
		})
	}
	modelRedirectCacheMu.RUnlock()
	for _, it := range items {
		fn(it.name, it.groups, it.displaySource)
	}
}
