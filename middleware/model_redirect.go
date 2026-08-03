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
	if len(filtered) == 0 {
		abortWithOpenAiMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": clientModel}), types.ErrorCodeModelNotFound)
		return nil, "", "", true, true
	}
	// LB among reachable peers only (after path/channel filter).
	filtered = model.OrderRedirectCandidates(filtered)
	common.SetContextKey(c, constant.ContextKeyModelRedirectActive, true)
	common.SetContextKey(c, constant.ContextKeyModelRedirectClientModel, clientModel)
	common.SetContextKey(c, constant.ContextKeyModelRedirectCandidates, filtered)
	first := filtered[0]
	ch, chErr := model.GetChannelForRedirect(first.ChannelID)
	if chErr != nil || ch == nil {
		abortWithOpenAiMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": clientModel}), types.ErrorCodeModelNotFound)
		return nil, "", "", true, true
	}
	// Billing/upstream identity for this hop is the attempt model.
	return ch, effectiveGroup, model.AttemptModel(clientModel, first), true, false
}
