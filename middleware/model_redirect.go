package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// tryModelRedirectSelection applies personal virtual-model redirect selection.
//
// Personal patch surface for Distribute(): keep only a thin call site in
// distributor.go so upstream affinity / auto-group edits rarely conflict.
//
// Returns:
//   - handled=false: not a redirect model; caller continues normal selection
//   - handled=true, aborted=false: channel/selectGroup/attemptModel filled
//   - handled=true, aborted=true: error already written; caller must return
func tryModelRedirectSelection(
	c *gin.Context,
	clientModel string,
	usingGroup string,
) (channel *model.Channel, selectGroup string, attemptModel string, handled bool, aborted bool) {
	effectiveGroup := usingGroup
	var cands []model.RedirectCandidate
	var ok bool
	if usingGroup == "auto" {
		// Virtual rules are bound to real groups; try token/user auto groups in order.
		userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		for _, g := range service.GetRequestAutoGroups(c, userGroup) {
			if cands, ok = model.ResolveModelRedirect(clientModel, g); ok {
				effectiveGroup = g
				common.SetContextKey(c, constant.ContextKeyAutoGroup, g)
				break
			}
		}
	} else {
		cands, ok = model.ResolveModelRedirect(clientModel, usingGroup)
	}
	if !ok {
		return nil, "", "", false, false
	}

	filtered := model.FilterRedirectCandidates(
		cands,
		clientModel,
		effectiveGroup,
		c.Request.URL.Path,
		channelSupportsRequestPath,
	)
	// Skip hops temporarily disabled after recent failures (1m * fails, max 30m).
	filtered = model.FilterRedirectCooldownDown(filtered, clientModel)
	// Personal model-redirect channel affinity: reuse the global
	// channel_affinity_setting rules. The rule's model_regex matches the virtual
	// model name; the bound channel is promoted within its same-priority pool.
	preferredID, _ := service.GetPreferredChannelByAffinity(c, clientModel, effectiveGroup)
	if preferredID > 0 {
		// Drop a stale binding when the bound channel is disabled. Cooldown-only
		// exclusions keep the channel enabled and must not clear the binding.
		preferred, chErr := model.CacheGetChannel(preferredID)
		if chErr != nil || preferred == nil {
			preferred, chErr = model.GetChannelById(preferredID, true)
		}
		if chErr == nil && preferred != nil && preferred.Status != common.ChannelStatusEnabled {
			if !service.ShouldKeepChannelAffinityOnChannelDisabled() {
				service.ClearCurrentChannelAffinityCache(c)
			}
			preferredID = 0
		}
	}
	if len(filtered) == 0 {
		abortWithRelayMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": clientModel}), types.ErrorCodeModelNotFound)
		return nil, "", "", true, true
	}
	// LB among reachable peers only (after path/channel/cooldown filter), with the
	// bound channel promoted within its equal-priority run.
	filtered = model.OrderRedirectCandidatesWithAffinity(filtered, preferredID)
	// Pick the first usable candidate. Model-only hops resolve a channel through
	// the channel layer for the mapped model at pick time; channel-bound hops load
	// the concrete channel. Earlier unusable hops are excluded from the retry list.
	firstCh, firstModel, firstIdx := model.FirstUsableRedirectCandidate(filtered, clientModel, effectiveGroup, c.Request.URL.Path)
	if firstCh == nil {
		abortWithRelayMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": clientModel}), types.ErrorCodeModelNotFound)
		return nil, "", "", true, true
	}
	if preferredID > 0 && filtered[firstIdx].ChannelID == preferredID {
		service.MarkChannelAffinityUsed(c, effectiveGroup, preferredID)
	}
	// Redirect HA must never be suppressed by a rule's SkipRetryOnFailure: retries
	// keep walking the same-priority pool, then lower priorities. Cleared last
	// because MarkChannelAffinityUsed re-applies the rule's meta.SkipRetry.
	service.ClearChannelAffinitySkipRetry(c)
	common.SetContextKey(c, constant.ContextKeyModelRedirectActive, true)
	common.SetContextKey(c, constant.ContextKeyModelRedirectClientModel, clientModel)
	common.SetContextKey(c, constant.ContextKeyModelRedirectCandidates, filtered[firstIdx:])
	// The resolved concrete group for auto-group requests; the retry slot walk
	// needs it to select channels for model-only hops.
	common.SetContextKey(c, constant.ContextKeyModelRedirectGroup, effectiveGroup)
	// Billing/upstream identity for this hop is the attempt model.
	return firstCh, effectiveGroup, firstModel, true, false
}
