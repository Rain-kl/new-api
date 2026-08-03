package constant

// Personal / site-local channel & API type IDs.
//
// These intentionally live outside the upstream sequential blocks (and outside
// ChannelTypeDummy / APITypeDummy) so merging upstream never collides on ID
// assignment. Prefer the high private range 1000+ for future personal types.
const (
	// ChannelTypeEcho is a local debug channel that echoes the user message.
	ChannelTypeEcho = 1000
	// APITypeEcho pairs with ChannelTypeEcho for adaptor dispatch.
	APITypeEcho = 1000
)

// Personal gin context keys (model redirect, etc.).
const (
	ContextKeyModelRedirectActive      ContextKey = "model_redirect_active"
	ContextKeyModelRedirectClientModel ContextKey = "model_redirect_client_model"
	ContextKeyModelRedirectCandidates  ContextKey = "model_redirect_candidates"

	// ModelRedirectSentinelChannelID marks a model-redirect target as a nested
	// virtual-model reference. Not a channels row; target.model holds the child name.
	ModelRedirectSentinelChannelID = -1
)

func init() {
	// Map registration avoids editing the upstream ChannelTypeNames literal.
	ChannelTypeNames[ChannelTypeEcho] = "Echo"
}
