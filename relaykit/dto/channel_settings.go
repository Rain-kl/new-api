package dto

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/relaykit/types"
)

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	ProxyId                int    `json:"proxy_id,omitempty"` // 0 = not bound to managed proxy pool
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
	// HTTPProtocol controls outbound HTTP version negotiation for this channel.
	// Accepted values: "", "auto" (default), "http1".
	HTTPProtocol string `json:"http_protocol,omitempty"`
	// HTTP2ConnectionShards spreads HTTP/2 traffic across N independent transports
	// (1-8). Zero/unset means 1. Ignored when HTTPProtocol is "http1".
	HTTP2ConnectionShards int `json:"http2_connection_shards,omitempty"`
	// MessagesRoleCompatibilityEnabled remaps unsupported message roles before
	// the request is sent upstream (e.g. "developer" -> fallback role).
	MessagesRoleCompatibilityEnabled bool `json:"messages_role_compatibility_enabled,omitempty"`
	// MessagesRoleAllowedList is the set of roles accepted by the upstream.
	// Empty means DefaultMessagesRoleAllowedList when compatibility is enabled.
	MessagesRoleAllowedList []string `json:"messages_role_allowed_list,omitempty"`
	// MessagesRoleFallback is used when a message role is not in the allowed list.
	// Empty means DefaultMessagesRoleFallback when compatibility is enabled.
	MessagesRoleFallback string `json:"messages_role_fallback,omitempty"`

	// CodexCompatEnabled enables Sub2API Codex-compatible identity headers/body
	// shaping for type-59 channels. Default false (no behavior change).
	CodexCompatEnabled bool `json:"codex_compat_enabled,omitempty"`
	// CodexClientVersion is the synthetic client version (X.Y.Z[+suffix]) used
	// when synthesizing User-Agent. Required when compat is enabled and mode is
	// auto or synthesize.
	CodexClientVersion string `json:"codex_client_version,omitempty"`
	// CodexClientName is the synthetic originator / UA client segment.
	// Empty defaults to DefaultCodexClientName at runtime.
	CodexClientName string `json:"codex_client_name,omitempty"`
	// CodexIdentityMode controls identity header policy: auto|passthrough|synthesize.
	// Empty is treated as auto.
	CodexIdentityMode string `json:"codex_identity_mode,omitempty"`

	// ChatCompletionsToResponses converts inbound Chat Completions requests to
	// the OpenAI Responses API before calling upstream. Intended for Sub2API
	// (and similar) gateways that prefer /v1/responses. Default false.
	// When true, all chat-completions traffic on this channel is converted
	// regardless of the global chat_completions_to_responses_policy.
	ChatCompletionsToResponses bool `json:"chat_completions_to_responses,omitempty"`

	// ReasoningEffortRulesEnabled enables per-model reasoning effort rules on this channel.
	ReasoningEffortRulesEnabled bool `json:"reasoning_effort_rules_enabled,omitempty"`
	// ReasoningEffortRules matches OriginModelName (client model or mapping source key).
	ReasoningEffortRules []ReasoningEffortRule `json:"reasoning_effort_rules,omitempty"`
}

// ReasoningEffortRule is a per-model reasoning effort override for a channel.
type ReasoningEffortRule struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Force  bool   `json:"force,omitempty"`
}

const (
	HTTPProtocolAuto         = "auto"
	HTTPProtocolHTTP1        = "http1"
	MaxHTTP2ConnectionShards = 8

	DefaultMessagesRoleFallback = "system"

	CodexIdentityModeAuto        = "auto"
	CodexIdentityModePassthrough = "passthrough"
	CodexIdentityModeSynthesize  = "synthesize"
	DefaultCodexClientName       = "codex_cli_rs"
)

// codexClientVersionPattern requires a leading numeric triple (X.Y.Z); optional
// suffix after the triple is allowed (e.g. 0.146.0-beta).
var codexClientVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// DefaultMessagesRoleAllowedList matches common OpenAI-compatible role enums that
// reject newer roles such as "developer".
var DefaultMessagesRoleAllowedList = []string{"system", "assistant", "user", "tool", "function"}

// ValidateHTTPTransport validates save-time HTTP transport channel settings.
func (s *ChannelSettings) ValidateHTTPTransport() error {
	if s == nil {
		return nil
	}
	protocol := strings.ToLower(strings.TrimSpace(s.HTTPProtocol))
	switch protocol {
	case "", HTTPProtocolAuto, HTTPProtocolHTTP1:
	default:
		return fmt.Errorf("invalid http_protocol: %s", s.HTTPProtocol)
	}
	if s.HTTP2ConnectionShards < 0 || s.HTTP2ConnectionShards > MaxHTTP2ConnectionShards {
		return fmt.Errorf("invalid http2_connection_shards: %d", s.HTTP2ConnectionShards)
	}
	if protocol == HTTPProtocolHTTP1 && s.HTTP2ConnectionShards > 1 {
		return fmt.Errorf("http2_connection_shards must be 1 when http_protocol is http1")
	}
	return nil
}

// ValidateMessagesRoleCompatibility validates role compatibility settings.
func (s *ChannelSettings) ValidateMessagesRoleCompatibility() error {
	if s == nil || !s.MessagesRoleCompatibilityEnabled {
		return nil
	}
	allowed := normalizeMessagesRoleList(s.MessagesRoleAllowedList)
	if len(allowed) == 0 {
		return fmt.Errorf("messages_role_allowed_list must not be empty when messages role compatibility is enabled")
	}
	fallback := strings.TrimSpace(s.MessagesRoleFallback)
	if fallback == "" {
		return fmt.Errorf("messages_role_fallback must not be empty when messages role compatibility is enabled")
	}
	for _, role := range allowed {
		if role == fallback {
			return nil
		}
	}
	return fmt.Errorf("messages_role_fallback %q must be included in messages_role_allowed_list", fallback)
}

// ValidateReasoningEffortRules validates per-model reasoning effort rules.
// Empty rules are always OK. When rules are present, model/effort must be non-empty
// after trim and model keys must be unique (case-sensitive, after trim).
func (s *ChannelSettings) ValidateReasoningEffortRules() error {
	if s == nil || len(s.ReasoningEffortRules) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(s.ReasoningEffortRules))
	for i, rule := range s.ReasoningEffortRules {
		model := strings.TrimSpace(rule.Model)
		effort := strings.TrimSpace(rule.Effort)
		if model == "" {
			return fmt.Errorf("reasoning_effort_rules[%d].model is required", i)
		}
		if effort == "" {
			return fmt.Errorf("reasoning_effort_rules[%d].effort is required", i)
		}
		if _, ok := seen[model]; ok {
			return fmt.Errorf("reasoning_effort_rules contains duplicate model: %s", model)
		}
		seen[model] = struct{}{}
	}
	return nil
}

// ValidateCodexCompat validates Sub2API Codex compatibility channel settings.
// No-op when CodexCompatEnabled is false.
func (s *ChannelSettings) ValidateCodexCompat() error {
	if s == nil || !s.CodexCompatEnabled {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(s.CodexIdentityMode))
	if mode == "" {
		mode = CodexIdentityModeAuto
	}
	switch mode {
	case CodexIdentityModeAuto, CodexIdentityModePassthrough, CodexIdentityModeSynthesize:
	default:
		return fmt.Errorf("invalid codex_identity_mode: %s", s.CodexIdentityMode)
	}
	if mode == CodexIdentityModeAuto || mode == CodexIdentityModeSynthesize {
		version := strings.TrimSpace(s.CodexClientVersion)
		if version == "" {
			return fmt.Errorf("codex_client_version is required when codex_compat is enabled with identity mode %s", mode)
		}
		if !codexClientVersionPattern.MatchString(version) {
			return fmt.Errorf("invalid codex_client_version: %s", s.CodexClientVersion)
		}
	}
	return nil
}

// ApplyMessagesRoleCompatibility remaps message roles that are not in the
// configured allowed list to the fallback role. No-op when disabled.
func (s *ChannelSettings) ApplyMessagesRoleCompatibility(messages []Message) {
	if s == nil || !s.MessagesRoleCompatibilityEnabled || len(messages) == 0 {
		return
	}
	allowed := normalizeMessagesRoleList(s.MessagesRoleAllowedList)
	if len(allowed) == 0 {
		allowed = append([]string(nil), DefaultMessagesRoleAllowedList...)
	}
	fallback := strings.TrimSpace(s.MessagesRoleFallback)
	if fallback == "" {
		fallback = DefaultMessagesRoleFallback
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, role := range allowed {
		allowedSet[role] = struct{}{}
	}
	for i := range messages {
		role := strings.TrimSpace(messages[i].Role)
		if role == "" {
			continue
		}
		if _, ok := allowedSet[role]; ok {
			continue
		}
		messages[i].Role = fallback
	}
}

func normalizeMessagesRoleList(roles []string) []string {
	if len(roles) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(roles))
	normalized := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		normalized = append(normalized, role)
	}
	return normalized
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string                `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType         `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool                 `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool                  `json:"claude_beta_query,omitempty"`          // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool                  `json:"allow_service_tier,omitempty"`         // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool                  `json:"allow_inference_geo,omitempty"`        // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool                  `json:"allow_speed,omitempty"`                // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool                  `json:"allow_safety_identifier,omitempty"`    // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool                  `json:"disable_store,omitempty"`              // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool                  `json:"allow_include_obfuscation,omitempty"`  // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	DisableTaskPollingSleep               bool                  `json:"disable_task_polling_sleep,omitempty"` // 是否跳过异步任务轮询间隔
	AwsKeyType                            AwsKeyType            `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                  `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool                  `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64                 `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string              `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string              `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string              `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
	AdvancedCustom                        *AdvancedCustomConfig `json:"advanced_custom,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

const (
	advancedCustomConverterNone                             = "none"
	advancedCustomConverterClaudeMessagesToOpenAIChat       = "anthropic_messages_to_openai_chat_completions"
	advancedCustomConverterOpenAIChatToClaudeMessages       = "openai_chat_completions_to_anthropic_messages"
	advancedCustomConverterOpenAIChatToOpenAIResponses      = "openai_chat_completions_to_openai_responses"
	advancedCustomConverterOpenAIResponsesToOpenAIChat      = "openai_responses_to_openai_chat_completions"
	advancedCustomConverterOpenAIResponsesToClaudeMessages  = "openai_responses_to_claude_messages"
	advancedCustomConverterOpenAIResponsesToGemini          = "openai_responses_to_gemini_generate_content"
	advancedCustomConverterGeminiContentToOpenAIChat        = "gemini_generate_content_to_openai_chat_completions"
	advancedCustomConverterOpenAIChatToGeminiContent        = "openai_chat_completions_to_gemini_generate_content"
)

const (
	AdvancedCustomAuthTypeNone   = "none"
	AdvancedCustomAuthTypeHeader = "header"
	AdvancedCustomAuthTypeQuery  = "query"
)

type AdvancedCustomConfig struct {
	Routes []AdvancedCustomRoute `json:"advanced_routes,omitempty"`
}

type AdvancedCustomRoute struct {
	IncomingPath string                   `json:"incoming_path,omitempty"`
	UpstreamPath string                   `json:"upstream_path,omitempty"`
	Converter    string                   `json:"converter,omitempty"`
	Models       []string                 `json:"models,omitempty"`
	Auth         *AdvancedCustomRouteAuth `json:"auth,omitempty"`
}

type AdvancedCustomRouteAuth struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

const (
	advancedCustomModelPlaceholder = "{model}"
	advancedCustomModelRegexPrefix = "re:"
)

const (
	advancedCustomEndpointPathOpenAIChat             = "/v1/chat/completions"
	advancedCustomEndpointPathOpenAIResponses        = "/v1/responses"
	advancedCustomEndpointPathOpenAIResponsesCompact = "/v1/responses/compact"
	advancedCustomEndpointPathOpenAIAlphaSearch      = "/v1/alpha/search"
	advancedCustomEndpointPathClaudeMessages         = "/v1/messages"
	advancedCustomEndpointPathJinaRerank             = "/v1/rerank"
	advancedCustomEndpointPathImageGeneration        = "/v1/images/generations"
	advancedCustomEndpointPathEmbeddings             = "/v1/embeddings"
)

// AdvancedCustomModelListPath identifies the optional OpenAI Models discovery route.
const AdvancedCustomModelListPath = "/v1/models"

// MatchPath returns the first route whose IncomingPath matches requestPath.
// Matching mirrors the relay adaptor: exact match, {model} placeholder, and
// :generateContent <-> :streamGenerateContent equivalence.
func (c *AdvancedCustomConfig) MatchPath(requestPath string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// MatchPathForModel returns the first route whose IncomingPath and Models match.
// An empty Models list is a catch-all fallback for that incoming path.
func (c *AdvancedCustomConfig) MatchPathForModel(requestPath string, model string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	model = strings.TrimSpace(model)
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) &&
			matchAdvancedCustomRouteModel(route.Models, model) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// ModelListRoute returns the explicitly configured OpenAI Models discovery route.
// Template routes that merely happen to match /v1/models are not discovery routes.
func (c *AdvancedCustomConfig) ModelListRoute() (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if strings.TrimSpace(route.IncomingPath) == AdvancedCustomModelListPath {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// SupportsPath reports whether any route matches requestPath.
func (c *AdvancedCustomConfig) SupportsPath(requestPath string) bool {
	_, ok := c.MatchPath(requestPath)
	return ok
}

// SupportsPathForModel reports whether any route matches requestPath and model.
func (c *AdvancedCustomConfig) SupportsPathForModel(requestPath string, model string) bool {
	_, ok := c.MatchPathForModel(requestPath, model)
	return ok
}

func (c *AdvancedCustomConfig) SupportedEndpointTypesForModel(model string) []types.EndpointType {
	if c == nil {
		return nil
	}
	model = strings.TrimSpace(model)
	endpoints := make([]types.EndpointType, 0, len(c.Routes))
	seen := make(map[types.EndpointType]struct{}, len(c.Routes))
	for _, route := range c.Routes {
		if !matchAdvancedCustomRouteModel(route.Models, model) {
			continue
		}
		endpointType, ok := advancedCustomEndpointTypeFromIncomingPath(strings.TrimSpace(route.IncomingPath))
		if !ok {
			continue
		}
		if _, exists := seen[endpointType]; exists {
			continue
		}
		seen[endpointType] = struct{}{}
		endpoints = append(endpoints, endpointType)
	}
	return endpoints
}

func advancedCustomEndpointTypeFromIncomingPath(incomingPath string) (types.EndpointType, bool) {
	switch incomingPath {
	case advancedCustomEndpointPathOpenAIChat:
		return types.EndpointTypeOpenAI, true
	case advancedCustomEndpointPathOpenAIResponses:
		return types.EndpointTypeOpenAIResponse, true
	case advancedCustomEndpointPathOpenAIResponsesCompact:
		return types.EndpointTypeOpenAIResponseCompact, true
	case advancedCustomEndpointPathOpenAIAlphaSearch:
		return types.EndpointTypeOpenAIAlphaSearch, true
	case advancedCustomEndpointPathClaudeMessages:
		return types.EndpointTypeAnthropic, true
	case advancedCustomEndpointPathJinaRerank:
		return types.EndpointTypeJinaRerank, true
	case advancedCustomEndpointPathImageGeneration:
		return types.EndpointTypeImageGeneration, true
	case advancedCustomEndpointPathEmbeddings:
		return types.EndpointTypeEmbeddings, true
	default:
		if isAdvancedCustomGeminiIncomingPath(incomingPath) {
			return types.EndpointTypeGemini, true
		}
		return "", false
	}
}

func isAdvancedCustomGeminiIncomingPath(incomingPath string) bool {
	if !strings.HasPrefix(incomingPath, "/v1beta/models/") {
		return false
	}
	return strings.Contains(incomingPath, ":generateContent") || strings.Contains(incomingPath, ":streamGenerateContent")
}

func matchAdvancedCustomRouteModel(models []string, model string) bool {
	normalizedModels := normalizeAdvancedCustomRouteModels(models)
	if len(normalizedModels) == 0 {
		return true
	}
	for _, allowedModel := range normalizedModels {
		if matchAdvancedCustomRouteModelRule(allowedModel, model) {
			return true
		}
	}
	return false
}

// advancedCustomModelRegexCache caches compiled route model patterns. Route model
// matching runs on the request hot path (distributor affinity, ability filtering,
// channel cache filtering, adaptor resolve), so patterns must not be recompiled per
// request. Invalid patterns are cached as nil to avoid recompiling them as well.
var advancedCustomModelRegexCache sync.Map // pattern string -> *regexp.Regexp (nil when invalid)

func compileAdvancedCustomModelRegex(pattern string) *regexp.Regexp {
	if cached, ok := advancedCustomModelRegexCache.Load(pattern); ok {
		re, _ := cached.(*regexp.Regexp)
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	advancedCustomModelRegexCache.Store(pattern, re)
	return re
}

func matchAdvancedCustomRouteModelRule(rule string, model string) bool {
	if !strings.HasPrefix(rule, advancedCustomModelRegexPrefix) {
		return rule == model
	}
	pattern := strings.TrimPrefix(rule, advancedCustomModelRegexPrefix)
	if pattern == "" {
		return false
	}
	re := compileAdvancedCustomModelRegex(pattern)
	return re != nil && re.MatchString(model)
}

func matchAdvancedCustomIncomingPath(configuredPath string, requestPath string) bool {
	if matchAdvancedCustomIncomingPathTemplate(configuredPath, requestPath) {
		return true
	}
	if strings.Contains(configuredPath, ":generateContent") {
		streamPath := strings.Replace(configuredPath, ":generateContent", ":streamGenerateContent", 1)
		return matchAdvancedCustomIncomingPathTemplate(streamPath, requestPath)
	}
	return false
}

func matchAdvancedCustomIncomingPathTemplate(configuredPath string, requestPath string) bool {
	if !strings.Contains(configuredPath, advancedCustomModelPlaceholder) {
		return configuredPath == requestPath
	}

	parts := strings.Split(configuredPath, advancedCustomModelPlaceholder)
	if len(parts) != 2 {
		return false
	}
	if !strings.HasPrefix(requestPath, parts[0]) || !strings.HasSuffix(requestPath, parts[1]) {
		return false
	}

	model := strings.TrimSuffix(strings.TrimPrefix(requestPath, parts[0]), parts[1])
	return model != "" && !strings.Contains(model, "/")
}

func IsAdvancedCustomConverterAllowed(converter string) bool {
	switch converter {
	case advancedCustomConverterNone,
		advancedCustomConverterClaudeMessagesToOpenAIChat,
		advancedCustomConverterOpenAIChatToClaudeMessages,
		advancedCustomConverterOpenAIChatToOpenAIResponses,
		advancedCustomConverterOpenAIResponsesToOpenAIChat,
		advancedCustomConverterOpenAIResponsesToClaudeMessages,
		advancedCustomConverterOpenAIResponsesToGemini,
		advancedCustomConverterGeminiContentToOpenAIChat,
		advancedCustomConverterOpenAIChatToGeminiContent:
		return true
	default:
		return false
	}
}

func (c *AdvancedCustomConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("advanced_custom is required")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("advanced_custom requires at least one route")
	}

	paths := make(map[string]*advancedCustomPathModelState, len(c.Routes))
	modelListRouteIndex := -1
	for i := range c.Routes {
		route := c.Routes[i]
		route.IncomingPath = strings.TrimSpace(route.IncomingPath)
		upstreamPath := strings.TrimSpace(route.UpstreamPath)
		route.Converter = strings.TrimSpace(route.Converter)
		if route.Converter == "" {
			route.Converter = advancedCustomConverterNone
		}

		if route.IncomingPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path is required", i)
		}
		if !strings.HasPrefix(route.IncomingPath, "/") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must start with /", i)
		}
		if strings.Contains(route.IncomingPath, "?") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must not include query", i)
		}
		if route.IncomingPath == AdvancedCustomModelListPath {
			if modelListRouteIndex >= 0 {
				return fmt.Errorf("advanced_custom.advanced_routes[%d] duplicates the /v1/models route at advanced_routes[%d]", i, modelListRouteIndex)
			}
			modelListRouteIndex = i
			if len(normalizeAdvancedCustomRouteModels(route.Models)) > 0 {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].models must be empty for /v1/models", i)
			}
			if route.Converter != advancedCustomConverterNone {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].converter must be none for /v1/models", i)
			}
			if strings.Contains(upstreamPath, advancedCustomModelPlaceholder) {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must not contain %s for /v1/models", i, advancedCustomModelPlaceholder)
			}
		}
		if err := validateAdvancedCustomRouteModels(i, route.IncomingPath, route.Models, paths); err != nil {
			return err
		}

		if upstreamPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path is required", i)
		}
		if err := validateAdvancedCustomUpstreamTarget(i, upstreamPath); err != nil {
			return err
		}

		if !IsAdvancedCustomConverterAllowed(route.Converter) {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].converter is not registered: %s", i, route.Converter)
		}
		if err := validateAdvancedCustomConverterPath(i, route.IncomingPath, route.Converter); err != nil {
			return err
		}
		if err := validateAdvancedCustomRouteAuth(i, route.Auth); err != nil {
			return err
		}
	}

	return nil
}

type advancedCustomPathModelState struct {
	catchAllIndex int
	modelIndexes  map[string]int
}

func validateAdvancedCustomRouteModels(index int, incomingPath string, models []string, paths map[string]*advancedCustomPathModelState) error {
	state := paths[incomingPath]
	if state == nil {
		state = &advancedCustomPathModelState{
			catchAllIndex: -1,
			modelIndexes:  make(map[string]int),
		}
		paths[incomingPath] = state
	}

	normalizedModels := normalizeAdvancedCustomRouteModels(models)
	if len(normalizedModels) == 0 {
		if state.catchAllIndex >= 0 {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models catch-all already exists for incoming_path: %s", index, incomingPath)
		}
		state.catchAllIndex = index
		return nil
	}

	if state.catchAllIndex >= 0 {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models catch-all route must be last for incoming_path: %s", index, incomingPath)
	}

	seenInRoute := make(map[string]struct{}, len(normalizedModels))
	for _, model := range normalizedModels {
		if err := validateAdvancedCustomRouteModelRule(index, incomingPath, model); err != nil {
			return err
		}
		if _, exists := seenInRoute[model]; exists {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models contains duplicate model for incoming_path %s: %s", index, incomingPath, model)
		}
		seenInRoute[model] = struct{}{}
		if existingIndex, exists := state.modelIndexes[model]; exists {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models overlaps with advanced_routes[%d] for incoming_path %s: %s", index, existingIndex, incomingPath, model)
		}
		state.modelIndexes[model] = index
	}
	return nil
}

func validateAdvancedCustomRouteModelRule(index int, incomingPath string, model string) error {
	if !strings.HasPrefix(model, advancedCustomModelRegexPrefix) {
		return nil
	}
	pattern := strings.TrimPrefix(model, advancedCustomModelRegexPrefix)
	if pattern == "" {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models regex is empty for incoming_path %s: %s", index, incomingPath, model)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models regex is invalid for incoming_path %s: %s", index, incomingPath, model)
	}
	return nil
}

func normalizeAdvancedCustomRouteModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model != "" {
			normalized = append(normalized, model)
		}
	}
	return normalized
}

func validateAdvancedCustomUpstreamTarget(index int, upstreamPath string) error {
	if strings.HasPrefix(upstreamPath, "/") {
		if strings.HasPrefix(upstreamPath, "//") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
		}
		return nil
	}

	parsedURL, err := url.Parse(upstreamPath)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must use http or https", index)
	}
	return nil
}

func validateAdvancedCustomConverterPath(index int, incomingPath string, converter string) error {
	if incomingPath == advancedCustomEndpointPathOpenAIAlphaSearch {
		if converter == advancedCustomConverterNone {
			return nil
		}
		return fmt.Errorf("advanced_custom.advanced_routes[%d].converter does not match incoming_path: %s", index, converter)
	}
	switch converter {
	case advancedCustomConverterNone:
		return nil
	case advancedCustomConverterClaudeMessagesToOpenAIChat:
		if incomingPath == "/v1/messages" {
			return nil
		}
	case advancedCustomConverterOpenAIChatToClaudeMessages,
		advancedCustomConverterOpenAIChatToOpenAIResponses,
		advancedCustomConverterOpenAIChatToGeminiContent:
		if incomingPath == "/v1/chat/completions" {
			return nil
		}
	case advancedCustomConverterOpenAIResponsesToOpenAIChat,
		advancedCustomConverterOpenAIResponsesToClaudeMessages,
		advancedCustomConverterOpenAIResponsesToGemini:
		if incomingPath == "/v1/responses" {
			return nil
		}
	case advancedCustomConverterGeminiContentToOpenAIChat:
		if strings.Contains(incomingPath, ":generateContent") || strings.Contains(incomingPath, ":streamGenerateContent") {
			return nil
		}
	}
	return fmt.Errorf("advanced_custom.advanced_routes[%d].converter does not match incoming_path: %s", index, converter)
}

func validateAdvancedCustomRouteAuth(index int, auth *AdvancedCustomRouteAuth) error {
	if auth == nil {
		return nil
	}
	authType := strings.TrimSpace(auth.Type)
	switch authType {
	case AdvancedCustomAuthTypeNone:
		return nil
	case AdvancedCustomAuthTypeHeader, AdvancedCustomAuthTypeQuery:
		if strings.TrimSpace(auth.Name) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.name is required", index)
		}
		if strings.TrimSpace(auth.Value) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.value is required", index)
		}
		return nil
	default:
		return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.type is invalid: %s", index, auth.Type)
	}
}
