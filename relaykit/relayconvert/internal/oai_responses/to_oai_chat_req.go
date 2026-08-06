package oairesponses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const (
	responsesInputTypeFunctionCall       = "function_call"
	responsesInputTypeFunctionCallOutput = "function_call_output"
	responsesInputTypeCustomToolCall     = "custom_tool_call"
	responsesInputTypeCustomToolOutput   = "custom_tool_call_output"
	responsesInputTypeToolSearchCall     = "tool_search_call"
	responsesInputTypeToolSearchOutput   = "tool_search_output"
)

const (
	ResponsesInputTypeFunctionCall       = responsesInputTypeFunctionCall
	ResponsesInputTypeFunctionCallOutput = responsesInputTypeFunctionCallOutput
	ResponsesInputTypeCustomToolCall     = responsesInputTypeCustomToolCall
	ResponsesInputTypeCustomToolOutput   = responsesInputTypeCustomToolOutput
)

// Codex Responses tool carriers that Chat Completions upstreams reject unless
// normalized. Aligned with cc-switch transform_codex_chat.
const (
	responsesToolTypeFunction   = "function"
	responsesToolTypeCustom     = "custom"
	responsesToolTypeToolSearch = "tool_search"
	responsesToolTypeNamespace  = "namespace"

	toolSearchProxyName               = "tool_search"
	customToolInputField              = "input"
	chatToolNameMaxLen                = 64
	customToolInputDescription        = "Raw string input for the original custom tool. Preserve formatting exactly and follow the original tool definition embedded in the description."
	customToolPreservedMetadataHeader = "Original tool definition:"
)

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return ResponsesRequestToChatCompletionsRequestWithMeta(req, nil)
}

// ResponsesRequestToChatCompletionsRequestWithMeta converts Responses → Chat
// and, when meta is non-nil, stores the Codex tool bridge for response reverse
// mapping (custom / tool_search / namespace).
func ResponsesRequestToChatCompletionsRequestWithMeta(req *dto.OpenAIResponsesRequest, meta convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if err := validateResponsesRequestChatUnsupportedFields(req); err != nil {
		return nil, err
	}

	toolCtx, err := buildCodexToolContextFromRequest(req)
	if err != nil {
		return nil, err
	}
	AttachCodexToolBridge(meta, toolCtx)

	messages, err := responsesRequestMessagesToChat(req, toolCtx)
	if err != nil {
		return nil, err
	}

	toolChoice, err := responsesRequestToolChoiceToChat(req.ToolChoice, toolCtx)
	if err != nil {
		return nil, err
	}

	responseFormat, err := responsesRequestTextToChatResponseFormat(req.Text)
	if err != nil {
		return nil, err
	}

	tools := toolCtx.chatTools()
	out := &dto.GeneralOpenAIRequest{
		Model:                req.Model,
		Messages:             messages,
		Stream:               req.Stream,
		StreamOptions:        req.StreamOptions,
		MaxCompletionTokens:  req.MaxOutputTokens,
		Temperature:          req.Temperature,
		TopP:                 req.TopP,
		TopLogProbs:          req.TopLogProbs,
		ResponseFormat:       responseFormat,
		Tools:                tools,
		ToolChoice:           toolChoice,
		User:                 req.User,
		Store:                req.Store,
		Metadata:             req.Metadata,
		SafetyIdentifier:     req.SafetyIdentifier,
		PromptCacheRetention: req.PromptCacheRetention,
		EnableThinking:       req.EnableThinking,
		ThinkingBudget:       req.ThinkingBudget,
	}

	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	if req.ServiceTier != "" {
		out.ServiceTier, _ = kitutil.Marshal(req.ServiceTier)
	}
	if len(req.ParallelToolCalls) > 0 && kitutil.GetJsonType(req.ParallelToolCalls) == "boolean" {
		var parallelToolCalls bool
		if err := kitutil.Unmarshal(req.ParallelToolCalls, &parallelToolCalls); err == nil {
			out.ParallelTooCalls = &parallelToolCalls
		}
	}
	if len(req.PromptCacheKey) > 0 && kitutil.GetJsonType(req.PromptCacheKey) == "string" {
		var promptCacheKey string
		if err := kitutil.Unmarshal(req.PromptCacheKey, &promptCacheKey); err == nil {
			out.PromptCacheKey = promptCacheKey
		}
	}

	// Strict OpenAI-compatible upstreams reject tool_choice / parallel_tool_calls
	// without a non-empty tools array (cc-switch same guard).
	if len(tools) == 0 {
		out.Tools = nil
		out.ToolChoice = nil
		out.ParallelTooCalls = nil
	}

	return out, nil
}

func validateResponsesRequestChatUnsupportedFields(req *dto.OpenAIResponsesRequest) error {
	unsupported := make([]string, 0, 4)
	if rawJSONPresent(req.Conversation) {
		unsupported = append(unsupported, "conversation")
	}
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		unsupported = append(unsupported, "previous_response_id")
	}
	if rawJSONPresent(req.Prompt) {
		unsupported = append(unsupported, "prompt")
	}
	if rawJSONPresent(req.ContextManagement) {
		unsupported = append(unsupported, "context_management")
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("responses to chat conversion does not support stateful fields: %s", strings.Join(unsupported, ", "))
	}
	return nil
}

func ValidateRequestChatUnsupportedFields(req *dto.OpenAIResponsesRequest) error {
	return validateResponsesRequestChatUnsupportedFields(req)
}

func responsesRequestMessagesToChat(req *dto.OpenAIResponsesRequest, toolCtx *codexToolContext) ([]dto.Message, error) {
	if toolCtx == nil {
		toolCtx = newCodexToolContext()
	}
	messages := make([]dto.Message, 0)
	if rawJSONPresent(req.Instructions) {
		instructions, err := responsesJSONString(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("invalid instructions: %w", err)
		}
		if strings.TrimSpace(instructions) != "" {
			messages = append(messages, dto.Message{Role: "system", Content: instructions})
		}
	}

	if !rawJSONPresent(req.Input) {
		return messages, nil
	}

	switch kitutil.GetJsonType(req.Input) {
	case "string":
		input, err := responsesJSONString(req.Input)
		if err != nil {
			return nil, fmt.Errorf("invalid input string: %w", err)
		}
		messages = append(messages, dto.Message{Role: "user", Content: input})
		return messages, nil
	case "array":
		var items []map[string]any
		if err := kitutil.Unmarshal(req.Input, &items); err != nil {
			return nil, fmt.Errorf("invalid input array: %w", err)
		}
		for _, item := range items {
			nextMessages, err := responsesInputItemToChatMessages(item, messages, toolCtx)
			if err != nil {
				return nil, err
			}
			messages = nextMessages
		}
		return messages, nil
	default:
		return nil, fmt.Errorf("unsupported responses input type %q", kitutil.GetJsonType(req.Input))
	}
}

func responsesInputItemToChatMessages(item map[string]any, messages []dto.Message, toolCtx *codexToolContext) ([]dto.Message, error) {
	itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
	switch itemType {
	case responsesInputTypeFunctionCall:
		toolCall, err := responsesFunctionCallItemToChatToolCall(item, toolCtx)
		if err != nil {
			return nil, err
		}
		return appendToolCallToLastAssistant(messages, toolCall), nil
	case responsesInputTypeCustomToolCall:
		toolCall, err := responsesCustomToolCallItemToChatToolCall(item)
		if err != nil {
			return nil, err
		}
		return appendToolCallToLastAssistant(messages, toolCall), nil
	case responsesInputTypeToolSearchCall:
		toolCall, err := responsesToolSearchCallItemToChatToolCall(item)
		if err != nil {
			return nil, err
		}
		return appendToolCallToLastAssistant(messages, toolCall), nil
	case responsesInputTypeFunctionCallOutput:
		callID := strings.TrimSpace(kitutil.Interface2String(item["call_id"]))
		content := responseToolOutputToChatContent(item["output"])
		return append(messages, dto.Message{Role: "tool", ToolCallId: callID, Content: content}), nil
	case responsesInputTypeCustomToolOutput, responsesInputTypeToolSearchOutput:
		// Match cc-switch: keep the whole item payload as tool content.
		callID := strings.TrimSpace(kitutil.Interface2String(item["call_id"]))
		content := responseToolOutputToChatContent(item)
		return append(messages, dto.Message{Role: "tool", ToolCallId: callID, Content: content}), nil
	}

	role := strings.TrimSpace(kitutil.Interface2String(item["role"]))
	if role == "" {
		// Items with an explicit Responses item type that we don't map above
		// (for example reasoning) are not chat messages.
		if itemType != "" && itemType != "message" {
			return messages, nil
		}
		role = "user"
	}
	content, err := responsesInputContentToChatContent(item["content"])
	if err != nil {
		return nil, err
	}
	return append(messages, dto.Message{Role: role, Content: content}), nil
}

func responsesInputContentToChatContent(content any) (any, error) {
	if content == nil {
		return "", nil
	}

	switch value := content.(type) {
	case string:
		return value, nil
	case []any:
		return responsesContentPartsToChatContent(value)
	case []map[string]any:
		parts := make([]any, 0, len(value))
		for _, part := range value {
			parts = append(parts, part)
		}
		return responsesContentPartsToChatContent(parts)
	default:
		return content, nil
	}
}

func responsesContentPartsToChatContent(parts []any) (any, error) {
	chatParts := make([]any, 0, len(parts))
	var textOnly strings.Builder
	onlyText := true

	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			onlyText = false
			chatParts = append(chatParts, rawPart)
			continue
		}

		partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
		switch partType {
		case "input_text", "output_text", "text":
			text := kitutil.Interface2String(part["text"])
			textOnly.WriteString(text)
			chatParts = append(chatParts, map[string]any{
				"type": dto.ContentTypeText,
				"text": text,
			})
		case "input_image":
			onlyText = false
			chatParts = append(chatParts, map[string]any{
				"type":      dto.ContentTypeImageURL,
				"image_url": responsesImagePartToChatImageURL(part),
			})
		case "input_file":
			onlyText = false
			chatParts = append(chatParts, map[string]any{
				"type": dto.ContentTypeFile,
				"file": responsesFilePartToChatFile(part),
			})
		case "input_audio":
			onlyText = false
			chatParts = append(chatParts, map[string]any{
				"type":        dto.ContentTypeInputAudio,
				"input_audio": responsesPartPayload(part, "input_audio"),
			})
		case "input_video":
			onlyText = false
			chatParts = append(chatParts, map[string]any{
				"type":      dto.ContentTypeVideoUrl,
				"video_url": responsesVideoPartToChatVideoURL(part),
			})
		default:
			onlyText = false
			chatParts = append(chatParts, part)
		}
	}

	if onlyText {
		return textOnly.String(), nil
	}
	return chatParts, nil
}

func responsesFunctionCallItemToChatToolCall(item map[string]any, toolCtx *codexToolContext) (dto.ToolCallRequest, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if name == "" {
		return dto.ToolCallRequest{}, errors.New("function_call item is missing name")
	}
	namespace := strings.TrimSpace(kitutil.Interface2String(item["namespace"]))
	chatName := name
	if toolCtx != nil {
		chatName = toolCtx.chatNameForResponseFunction(name, namespace)
	} else if namespace != "" {
		chatName = flattenNamespaceToolName(namespace, name)
	}
	return dto.ToolCallRequest{
		ID:   responsesCallID(item),
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      chatName,
			Arguments: responsesArgumentsString(item["arguments"]),
		},
	}, nil
}

func responsesCustomToolCallItemToChatToolCall(item map[string]any) (dto.ToolCallRequest, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	input := item["input"]
	if input == nil {
		input = ""
	}
	argsRaw, err := kitutil.Marshal(map[string]any{customToolInputField: input})
	if err != nil {
		return dto.ToolCallRequest{}, err
	}
	return dto.ToolCallRequest{
		ID:   responsesCallID(item),
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      name,
			Arguments: string(argsRaw),
		},
	}, nil
}

func responsesToolSearchCallItemToChatToolCall(item map[string]any) (dto.ToolCallRequest, error) {
	return dto.ToolCallRequest{
		ID:   responsesCallID(item),
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      toolSearchProxyName,
			Arguments: responsesArgumentsString(item["arguments"]),
		},
	}, nil
}

func appendToolCallToLastAssistant(messages []dto.Message, toolCall dto.ToolCallRequest) []dto.Message {
	if len(messages) == 0 || messages[len(messages)-1].Role != "assistant" {
		messages = append(messages, dto.Message{Role: "assistant"})
	}

	idx := len(messages) - 1
	toolCalls := messages[idx].ParseToolCalls()
	toolCalls = append(toolCalls, toolCall)
	toolCallsRaw, _ := kitutil.Marshal(toolCalls)
	messages[idx].ToolCalls = toolCallsRaw
	return messages
}

type codexToolKind int

const (
	codexToolKindFunction codexToolKind = iota
	codexToolKindNamespace
	codexToolKindCustom
	codexToolKindToolSearch
)

type codexToolSpec struct {
	kind      codexToolKind
	name      string
	namespace string
}

type codexToolContext struct {
	tools                    []dto.ToolCallRequest
	seenChatNames            map[string]struct{}
	chatNameToSpec           map[string]codexToolSpec
	namespaceNameToChatName  map[string]string
}

func newCodexToolContext() *codexToolContext {
	return &codexToolContext{
		tools:                   make([]dto.ToolCallRequest, 0),
		seenChatNames:           make(map[string]struct{}),
		chatNameToSpec:          make(map[string]codexToolSpec),
		namespaceNameToChatName: make(map[string]string),
	}
}

func (c *codexToolContext) chatTools() []dto.ToolCallRequest {
	if c == nil || len(c.tools) == 0 {
		return nil
	}
	return c.tools
}

func (c *codexToolContext) chatNameForResponseFunction(name, namespace string) string {
	name = strings.TrimSpace(name)
	namespace = strings.TrimSpace(namespace)
	if namespace != "" {
		if chatName, ok := c.namespaceNameToChatName[namespaceNameKey(namespace, name)]; ok {
			return chatName
		}
		return flattenNamespaceToolName(namespace, name)
	}
	return name
}

// AttachCodexToolBridge copies the request-side tool name map onto meta so Chat
// / Claude responses can restore custom_tool_call, tool_search_call, and
// namespaced function_call items.
func AttachCodexToolBridge(meta convmeta.Meta, toolCtx *codexToolContext) {
	if meta == nil || toolCtx == nil || len(toolCtx.chatNameToSpec) == 0 {
		return
	}
	bridge := meta.EnsureCodexToolBridge()
	if bridge.ChatNameToSpec == nil {
		bridge.ChatNameToSpec = make(map[string]convmeta.CodexToolSpec, len(toolCtx.chatNameToSpec))
	}
	for chatName, spec := range toolCtx.chatNameToSpec {
		bridge.Set(chatName, convmeta.CodexToolSpec{
			Kind:      codexToolKindToMeta(spec.kind),
			Name:      spec.name,
			Namespace: spec.namespace,
		})
	}
}

func codexToolKindToMeta(kind codexToolKind) convmeta.CodexToolKind {
	switch kind {
	case codexToolKindNamespace:
		return convmeta.CodexToolKindNamespace
	case codexToolKindCustom:
		return convmeta.CodexToolKindCustom
	case codexToolKindToolSearch:
		return convmeta.CodexToolKindToolSearch
	default:
		return convmeta.CodexToolKindFunction
	}
}

func (c *codexToolContext) addChatTool(chatName string, spec codexToolSpec, tool dto.ToolCallRequest) {
	chatName = strings.TrimSpace(chatName)
	if chatName == "" {
		return
	}
	if _, exists := c.seenChatNames[chatName]; exists {
		return
	}
	c.seenChatNames[chatName] = struct{}{}
	if spec.namespace != "" {
		c.namespaceNameToChatName[namespaceNameKey(spec.namespace, spec.name)] = chatName
	}
	c.chatNameToSpec[chatName] = spec
	c.tools = append(c.tools, tool)
}

func buildCodexToolContextFromRequest(req *dto.OpenAIResponsesRequest) (*codexToolContext, error) {
	ctx := newCodexToolContext()
	if req == nil {
		return ctx, nil
	}
	if rawJSONPresent(req.Tools) {
		var tools []any
		if err := kitutil.Unmarshal(req.Tools, &tools); err != nil {
			return nil, fmt.Errorf("invalid tools: %w", err)
		}
		for _, tool := range tools {
			if err := ctx.addResponseTool(tool); err != nil {
				return nil, err
			}
		}
	}
	if rawJSONPresent(req.Input) {
		if err := collectToolSearchOutputTools(req.Input, ctx); err != nil {
			return nil, err
		}
	}
	return ctx, nil
}

func (c *codexToolContext) addResponseTool(tool any) error {
	switch typed := tool.(type) {
	case string:
		name := strings.TrimSpace(typed)
		if name == "" {
			return nil
		}
		return c.addCustomTool(map[string]any{
			"type": responsesToolTypeCustom,
			"name": name,
		})
	case map[string]any:
		switch strings.TrimSpace(kitutil.Interface2String(typed["type"])) {
		case responsesToolTypeFunction:
			return c.addFunctionTool(typed, "")
		case responsesToolTypeCustom:
			return c.addCustomTool(typed)
		case responsesToolTypeToolSearch:
			c.addToolSearchTool()
			return nil
		case responsesToolTypeNamespace:
			return c.addNamespaceTool(typed)
		default:
			// Drop Responses built-ins / private carriers (web_search, local_shell, …).
			return nil
		}
	default:
		return nil
	}
}

func (c *codexToolContext) addFunctionTool(tool map[string]any, namespace string) error {
	originalName := responsesToolName(tool)
	if originalName == "" {
		return nil
	}
	chatName := originalName
	if namespace != "" {
		chatName = flattenNamespaceToolName(namespace, originalName)
	}
	chatTool, ok := responsesFunctionToolToChatTool(tool, chatName)
	if !ok {
		return nil
	}
	spec := codexToolSpec{
		kind: codexToolKindFunction,
		name: originalName,
	}
	if namespace != "" {
		spec.kind = codexToolKindNamespace
		spec.namespace = namespace
	}
	c.addChatTool(chatName, spec, chatTool)
	return nil
}

func (c *codexToolContext) addCustomTool(tool map[string]any) error {
	name := responsesToolName(tool)
	if name == "" {
		return nil
	}
	description, err := responsesCustomToolDescription(tool)
	if err != nil {
		return err
	}
	chatTool := dto.ToolCallRequest{
		Type: "function",
		Function: dto.FunctionRequest{
			Name:        name,
			Description: description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					customToolInputField: map[string]any{
						"type":        "string",
						"description": customToolInputDescription,
					},
				},
				"required": []any{customToolInputField},
			},
		},
	}
	c.addChatTool(name, codexToolSpec{kind: codexToolKindCustom, name: name}, chatTool)
	return nil
}

func (c *codexToolContext) addToolSearchTool() {
	chatTool := dto.ToolCallRequest{
		Type: "function",
		Function: dto.FunctionRequest{
			Name:        toolSearchProxyName,
			Description: "Search and load Codex tools, plugins, connectors, and MCP namespaces for the current task.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query for tools or connectors to load.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of tool groups to return.",
					},
				},
				"required": []any{"query"},
			},
		},
	}
	c.addChatTool(toolSearchProxyName, codexToolSpec{kind: codexToolKindToolSearch, name: toolSearchProxyName}, chatTool)
}

func (c *codexToolContext) addNamespaceTool(namespaceTool map[string]any) error {
	namespace := strings.TrimSpace(kitutil.Interface2String(namespaceTool["name"]))
	if namespace == "" {
		return nil
	}
	children := responsesToolChildren(namespaceTool)
	for _, child := range children {
		if strings.TrimSpace(kitutil.Interface2String(child["type"])) != responsesToolTypeFunction {
			continue
		}
		if err := c.addFunctionTool(child, namespace); err != nil {
			return err
		}
	}
	return nil
}

func collectToolSearchOutputTools(value any, ctx *codexToolContext) error {
	switch typed := value.(type) {
	case json.RawMessage:
		if !rawJSONPresent(typed) {
			return nil
		}
		var decoded any
		if err := kitutil.Unmarshal(typed, &decoded); err != nil {
			return nil
		}
		return collectToolSearchOutputTools(decoded, ctx)
	case []any:
		for _, item := range typed {
			if err := collectToolSearchOutputTools(item, ctx); err != nil {
				return err
			}
		}
		return nil
	case []map[string]any:
		for _, item := range typed {
			if err := collectToolSearchOutputTools(item, ctx); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		if strings.TrimSpace(kitutil.Interface2String(typed["type"])) == responsesInputTypeToolSearchOutput {
			if rawTools, ok := typed["tools"]; ok {
				switch tools := rawTools.(type) {
				case []any:
					for _, tool := range tools {
						if err := ctx.addResponseTool(tool); err != nil {
							return err
						}
					}
				case []map[string]any:
					for _, tool := range tools {
						if err := ctx.addResponseTool(tool); err != nil {
							return err
						}
					}
				}
			}
		}
		for _, child := range typed {
			if err := collectToolSearchOutputTools(child, ctx); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func responsesToolChildren(tool map[string]any) []map[string]any {
	for _, key := range []string{"tools", "children"} {
		raw, ok := tool[key]
		if !ok || raw == nil {
			continue
		}
		switch typed := raw.(type) {
		case []map[string]any:
			return typed
		case []any:
			out := make([]map[string]any, 0, len(typed))
			for _, item := range typed {
				if child, ok := item.(map[string]any); ok {
					out = append(out, child)
				}
			}
			return out
		}
	}
	return nil
}

func responsesToolName(tool map[string]any) string {
	if function, ok := tool["function"].(map[string]any); ok {
		if name := strings.TrimSpace(kitutil.Interface2String(function["name"])); name != "" {
			return name
		}
	}
	return strings.TrimSpace(kitutil.Interface2String(tool["name"]))
}

func responsesFunctionToolToChatTool(tool map[string]any, chatName string) (dto.ToolCallRequest, bool) {
	if strings.TrimSpace(kitutil.Interface2String(tool["type"])) != responsesToolTypeFunction {
		return dto.ToolCallRequest{}, false
	}
	if function, ok := tool["function"].(map[string]any); ok {
		fn := dto.FunctionRequest{
			Name:        chatName,
			Description: kitutil.Interface2String(function["description"]),
			Parameters:  normalizeFunctionParameters(function["parameters"]),
		}
		if fn.Description == "" {
			fn.Description = kitutil.Interface2String(tool["description"])
		}
		return dto.ToolCallRequest{Type: "function", Function: fn}, true
	}
	fn := dto.FunctionRequest{
		Name:        chatName,
		Description: kitutil.Interface2String(tool["description"]),
		Parameters:  normalizeFunctionParameters(tool["parameters"]),
	}
	return dto.ToolCallRequest{Type: "function", Function: fn}, true
}

func normalizeFunctionParameters(params any) any {
	switch typed := params.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed)+1)
		for k, v := range typed {
			out[k] = v
		}
		if strings.TrimSpace(kitutil.Interface2String(out["type"])) != "object" {
			out["type"] = "object"
		}
		return out
	case nil:
		return map[string]any{"type": "object", "properties": map[string]any{}}
	default:
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
}

func responsesCustomToolDescription(tool map[string]any) (string, error) {
	raw, err := kitutil.Marshal(tool)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString(customToolPreservedMetadataHeader)
	builder.WriteString("\n```json\n")
	builder.Write(raw)
	builder.WriteString("\n```")
	return builder.String(), nil
}

func flattenNamespaceToolName(namespace, name string) string {
	fullName := namespace + "__" + name
	if len(fullName) <= chatToolNameMaxLen {
		return fullName
	}
	sum := sha256.Sum256([]byte(fullName))
	hash := hex.EncodeToString(sum[:8])
	suffix := "__" + hash
	prefixLen := chatToolNameMaxLen - len(suffix)
	if prefixLen <= 0 {
		return hash
	}
	// Truncate on byte boundary; tool names are ASCII in practice.
	if prefixLen > len(fullName) {
		prefixLen = len(fullName)
	}
	return fullName[:prefixLen] + suffix
}

func namespaceNameKey(namespace, name string) string {
	return namespace + "\x00" + name
}

func responsesRequestToolChoiceToChat(raw json.RawMessage, toolCtx *codexToolContext) (any, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}
	if kitutil.GetJsonType(raw) == "string" {
		var choice string
		if err := kitutil.Unmarshal(raw, &choice); err != nil {
			return nil, fmt.Errorf("invalid tool_choice: %w", err)
		}
		return choice, nil
	}

	var choice map[string]any
	if err := kitutil.Unmarshal(raw, &choice); err != nil {
		return nil, fmt.Errorf("invalid tool_choice: %w", err)
	}
	choiceType := strings.TrimSpace(kitutil.Interface2String(choice["type"]))
	switch choiceType {
	case responsesToolTypeFunction:
		name := strings.TrimSpace(kitutil.Interface2String(choice["name"]))
		namespace := strings.TrimSpace(kitutil.Interface2String(choice["namespace"]))
		chatName := name
		if toolCtx != nil {
			chatName = toolCtx.chatNameForResponseFunction(name, namespace)
		} else if namespace != "" {
			chatName = flattenNamespaceToolName(namespace, name)
		}
		if chatName != "" {
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": chatName,
				},
			}, nil
		}
	case responsesToolTypeToolSearch:
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": toolSearchProxyName,
			},
		}, nil
	case responsesToolTypeCustom:
		name := strings.TrimSpace(kitutil.Interface2String(choice["name"]))
		if name != "" {
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": name,
				},
			}, nil
		}
	}
	return choice, nil
}

func RequestToolChoiceToChat(raw json.RawMessage) (any, error) {
	return responsesRequestToolChoiceToChat(raw, nil)
}

func responsesRequestTextToChatResponseFormat(raw json.RawMessage) (*dto.ResponseFormat, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}

	var textConfig map[string]any
	if err := kitutil.Unmarshal(raw, &textConfig); err != nil {
		return nil, fmt.Errorf("invalid text config: %w", err)
	}
	format, ok := textConfig["format"].(map[string]any)
	if !ok {
		return nil, nil
	}

	formatType := strings.TrimSpace(kitutil.Interface2String(format["type"]))
	if formatType == "" {
		return nil, nil
	}

	out := &dto.ResponseFormat{Type: formatType}
	if formatType == "json_schema" {
		schemaRaw, err := kitutil.Marshal(format)
		if err != nil {
			return nil, err
		}
		out.JsonSchema = schemaRaw
	}
	return out, nil
}

func RequestTextToChatResponseFormat(raw json.RawMessage) (*dto.ResponseFormat, error) {
	return responsesRequestTextToChatResponseFormat(raw)
}

func responsesImagePartToChatImageURL(part map[string]any) any {
	if imageURL, ok := part["image_url"]; ok {
		return imageURL
	}
	imageURL := map[string]any{}
	for _, key := range []string{"url", "file_id", "detail"} {
		if value, ok := part[key]; ok {
			imageURL[key] = value
		}
	}
	if len(imageURL) == 0 {
		return part
	}
	return imageURL
}

func responsesFilePartToChatFile(part map[string]any) any {
	if file, ok := part["file"]; ok {
		return file
	}
	file := map[string]any{}
	for _, key := range []string{"file_id", "file_data", "filename", "file_url"} {
		if value, ok := part[key]; ok {
			file[key] = value
		}
	}
	if len(file) == 0 {
		return part
	}
	return file
}

func responsesVideoPartToChatVideoURL(part map[string]any) any {
	if videoURL, ok := part["video_url"]; ok {
		if videoURLMap, ok := videoURL.(map[string]any); ok {
			if url := kitutil.Interface2String(videoURLMap["url"]); url != "" {
				return url
			}
		}
		return videoURL
	}
	if url := kitutil.Interface2String(part["url"]); url != "" {
		return url
	}
	return responsesPartPayload(part, "video_url")
}

func responsesPartPayload(part map[string]any, key string) any {
	if value, ok := part[key]; ok {
		return value
	}
	payload := make(map[string]any, len(part))
	for k, value := range part {
		if k == "type" {
			continue
		}
		payload[k] = value
	}
	return payload
}

func responsesCallID(item map[string]any) string {
	callID := strings.TrimSpace(kitutil.Interface2String(item["call_id"]))
	if callID != "" {
		return callID
	}
	return strings.TrimSpace(kitutil.Interface2String(item["id"]))
}

func CallID(item map[string]any) string {
	return responsesCallID(item)
}

func responsesArgumentsString(value any) string {
	switch v := value.(type) {
	case nil:
		// Strict OpenAI-compatible upstreams reject empty arguments strings.
		return "{}"
	case string:
		if strings.TrimSpace(v) == "" {
			return "{}"
		}
		return v
	default:
		raw, err := kitutil.Marshal(v)
		if err != nil {
			return kitutil.Interface2String(v)
		}
		return string(raw)
	}
}

func responseToolOutputToChatContent(value any) any {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		raw, err := kitutil.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(raw)
	}
}

func responsesJSONString(raw json.RawMessage) (string, error) {
	if kitutil.GetJsonType(raw) != "string" {
		return string(raw), nil
	}
	var value string
	if err := kitutil.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func rawJSONPresent(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	return kitutil.GetJsonType(raw) != "null"
}

func JSONString(raw json.RawMessage) (string, error) {
	return responsesJSONString(raw)
}

func RawJSONPresent(raw json.RawMessage) bool {
	return rawJSONPresent(raw)
}
