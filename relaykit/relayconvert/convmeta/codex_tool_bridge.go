package convmeta

// Codex tool kinds carried across Responses ↔ Chat/Claude conversion so
// upstream function-shaped tool calls can be restored to Codex Responses
// shapes (custom_tool_call / tool_search_call / namespaced function_call).
// Aligned with cc-switch CodexToolKind.
type CodexToolKind string

const (
	CodexToolKindFunction   CodexToolKind = "function"
	CodexToolKindNamespace  CodexToolKind = "namespace"
	CodexToolKindCustom     CodexToolKind = "custom"
	CodexToolKindToolSearch CodexToolKind = "tool_search"
)

// CodexToolSpec is the original Responses-side identity of a chat tool name.
type CodexToolSpec struct {
	Kind      CodexToolKind
	Name      string // original tool name (without namespace prefix)
	Namespace string // non-empty for namespaced tools
}

// CodexToolBridge maps chat-side tool names back to Codex Responses tool
// identities for the lifetime of one relay request/response.
type CodexToolBridge struct {
	ChatNameToSpec map[string]CodexToolSpec
}

// Lookup returns the Responses-side spec for a chat tool name.
func (b *CodexToolBridge) Lookup(chatName string) (CodexToolSpec, bool) {
	if b == nil || len(b.ChatNameToSpec) == 0 {
		return CodexToolSpec{}, false
	}
	spec, ok := b.ChatNameToSpec[chatName]
	return spec, ok
}

// IsCustom reports whether the chat tool name was synthesized from a Responses
// custom (freeform) tool.
func (b *CodexToolBridge) IsCustom(chatName string) bool {
	spec, ok := b.Lookup(chatName)
	return ok && spec.Kind == CodexToolKindCustom
}

// IsToolSearch reports whether the chat tool name is the Codex tool_search proxy.
func (b *CodexToolBridge) IsToolSearch(chatName string) bool {
	spec, ok := b.Lookup(chatName)
	return ok && spec.Kind == CodexToolKindToolSearch
}

// Set records a mapping from chat tool name to Responses identity.
func (b *CodexToolBridge) Set(chatName string, spec CodexToolSpec) {
	if b == nil || chatName == "" {
		return
	}
	if b.ChatNameToSpec == nil {
		b.ChatNameToSpec = make(map[string]CodexToolSpec)
	}
	b.ChatNameToSpec[chatName] = spec
}
