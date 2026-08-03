package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
)

// allowModelInUserList is the personal ListModels billing gate.
//
// Upstream only checks helper.HasModelBillingConfig. Virtual redirect models
// bill by the successful hop and often have no own price map — still list them.
// Keep this helper in a personal file so ListModels only needs a one-line call.
func allowModelInUserList(modelName string, acceptUnsetRatioModel bool) bool {
	if acceptUnsetRatioModel {
		return true
	}
	if helper.HasModelBillingConfig(modelName) {
		return true
	}
	return model.IsModelRedirectVirtual(modelName)
}
