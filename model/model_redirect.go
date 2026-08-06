package model

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
//
// Priority: larger number = higher priority. Targets with the same Priority form
// a load-balancing pool (currently equal share; Weight is reserved for future
// weighted balancing and is not applied yet).
type ModelRedirectTarget struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	RedirectId int    `json:"redirect_id" gorm:"index;not null"`
	Priority   int    `json:"priority" gorm:"not null;default:100"` // higher = preferred
	// Weight is reserved for future weighted LB among same-priority targets.
	// 0 means "equal share" (current behaviour). Non-zero values are stored but
	// not yet applied at resolve time.
	Weight    int    `json:"weight" gorm:"not null;default:0"`
	ChannelId int    `json:"channel_id" gorm:"not null;index"`
	Model     string `json:"model" gorm:"size:128;default:''"` // empty = passthrough
	Enabled   bool   `json:"enabled" gorm:"default:true"`
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

const (
	modelRedirectMaxPriority           = 1_000_000
	modelRedirectMaxTargets            = 64
	modelRedirectMaxModelLen           = 128
	modelRedirectMaxRemarkLen          = 255
	modelRedirectDefaultPrio           = 100
	modelRedirectMaxExpandDepth        = 32
	modelRedirectMaxExpandedCandidates = 128
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
		entry := &modelRedirectCacheEntry{
			Groups:  parseGroupSet(r.Groups),
			Targets: make([]RedirectCandidate, 0, len(r.Targets)),
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
		if len(entry.Targets) == 0 || len(entry.Groups) == 0 {
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

// expandModelRedirectTargets recursively expands nested virtual-model refs
// (sentinel ChannelID) into a flat list of real-channel candidates.
// Black-box order: full child chain before the parent's next target.
// Caller must not hold modelRedirectCacheMu; this function takes RLock as needed.
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

// ResolveModelRedirect returns a defensive copy of real-channel candidates when
// clientModel is a virtual model enabled for usingGroup. Nested sentinel targets
// are expanded black-box (full child chain before parent next target).
// ok=false means "not a redirect model" or expand yielded no usable real hops.
//
// Order is priority DESC within each redirect's cache entry (stable). Call
// OrderRedirectCandidates after filtering inaccessible channels so same-priority
// load balancing only covers live peers.
func ResolveModelRedirect(clientModel, usingGroup string) (cands []RedirectCandidate, ok bool) {
	clientModel = strings.TrimSpace(clientModel)
	usingGroup = strings.TrimSpace(usingGroup)
	if clientModel == "" {
		return nil, false
	}
	ensureModelRedirectCache()
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

// buildModelRedirectNestedEdgeMap builds name -> nested child names from rows.
// Only enabled targets with sentinel channel_id and non-empty model contribute edges.
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

// nestedEdgesFromInput returns enabled nested-ref child names from the save payload.
func nestedEdgesFromInput(targets []ModelRedirectTargetInput) []string {
	var children []string
	for _, t := range targets {
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
		edges[name] = nestedEdgesFromInput(in.Targets)
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
	targets := buildTargetsFromInput(in.Targets, 0)
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one redirect target is required")
	}
	row := &ModelRedirect{
		Name:      strings.TrimSpace(in.Name),
		Groups:    normalizeGroupList(in.Groups),
		Enabled:   enabled,
		Remark:    normalizeRemark(in.Remark),
		CreatedAt: now,
		UpdatedAt: now,
		Targets:   targets,
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
	targets := buildTargetsFromInput(in.Targets, id)
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one redirect target is required")
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	if err := tx.Model(&ModelRedirect{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":       name,
		"groups":     normalizeGroupList(in.Groups),
		"enabled":    enabled,
		"remark":     normalizeRemark(in.Remark),
		"updated_at": common.GetTimestamp(),
	}).Error; err != nil {
		tx.Rollback()
		return nil, err
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

// ModelRedirectDisplaySourceModel returns the first (highest-priority) target model
// or the virtual name for pricing/display when the virtual name itself has no ratio/price.
func ModelRedirectDisplaySourceModel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	ensureModelRedirectCache()
	modelRedirectCacheMu.RLock()
	entry := modelRedirectCache[name]
	if entry == nil || len(entry.Targets) == 0 {
		modelRedirectCacheMu.RUnlock()
		return name
	}
	first := entry.Targets[0]
	modelRedirectCacheMu.RUnlock()
	return AttemptModel(name, first)
}

// ForEachEnabledModelRedirect walks enabled virtual models (name + groups + first target).
// Callback runs without holding the cache lock so callers may do heavier work safely.
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
		if entry == nil || len(entry.Targets) == 0 {
			continue
		}
		groups := make([]string, 0, len(entry.Groups))
		for g := range entry.Groups {
			groups = append(groups, g)
		}
		items = append(items, snap{
			name:          name,
			groups:        groups,
			displaySource: AttemptModel(name, entry.Targets[0]),
		})
	}
	modelRedirectCacheMu.RUnlock()
	for _, it := range items {
		fn(it.name, it.groups, it.displaySource)
	}
}
