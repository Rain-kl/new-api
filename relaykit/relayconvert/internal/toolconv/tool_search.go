package toolconv

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// prepareResponsesToolSearch carries client-executed discovery through function-only
// protocols. Definitions loaded by a previous search join the upstream tool list.
// The original request remains untouched for retries and native Responses routes.
func prepareResponsesToolSearch(source *dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, map[string]struct{}, error) {
	var tools []json.RawMessage
	if len(source.Tools) > 0 {
		if err := kitutil.Unmarshal(source.Tools, &tools); err != nil {
			return nil, nil, err
		}
	}
	compatibilityChanged := false
	for index, raw := range tools {
		normalized, changed, err := normalizeCodexToolDefinition(raw)
		if err != nil {
			return nil, nil, err
		}
		tools[index] = normalized
		compatibilityChanged = compatibilityChanged || changed
	}
	if compatibilityChanged {
		clone := *source
		normalized, err := kitutil.Marshal(tools)
		if err != nil {
			return nil, nil, err
		}
		clone.Tools = normalized
		source = &clone
	}
	var items []map[string]json.RawMessage
	if len(source.Input) > 0 && kitutil.GetJsonType(source.Input) == "array" {
		if err := kitutil.Unmarshal(source.Input, &items); err != nil {
			return nil, nil, err
		}
	}
	hasSearch := false
	for _, raw := range tools {
		var tool struct {
			Type string `json:"type"`
		}
		if err := kitutil.Unmarshal(raw, &tool); err != nil {
			return nil, nil, err
		}
		hasSearch = hasSearch || tool.Type == "tool_search"
	}
	for _, item := range items {
		var kind string
		_ = kitutil.Unmarshal(item["type"], &kind)
		if kind == "tool_search_call" || kind == "tool_search_output" {
			hasSearch = true
		}
	}
	if !hasSearch {
		return source, nil, nil
	}
	proxyName := "tool_search"
	for {
		collision := false
		for _, raw := range tools {
			var tool struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			_ = kitutil.Unmarshal(raw, &tool)
			if tool.Type != "tool_search" && tool.Name == proxyName {
				collision = true
				break
			}
		}
		if !collision {
			break
		}
		proxyName += "_proxy"
	}
	normalized := make([]json.RawMessage, 0, len(tools)+1)
	for _, raw := range tools {
		var tool struct {
			Type string `json:"type"`
		}
		_ = kitutil.Unmarshal(raw, &tool)
		if tool.Type != "tool_search" {
			normalized = append(normalized, raw)
		}
	}
	search, err := kitutil.Marshal(map[string]any{"type": "function", "name": proxyName, "description": "Search and load Codex tools, plugins, connectors, and MCP namespaces for the current task.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": "Search query for tools or connectors to load."}, "limit": map[string]any{"type": "integer"}}, "required": []string{"query"}}})
	if err != nil {
		return nil, nil, err
	}
	normalized = append(normalized, search)
	for _, item := range items {
		var kind string
		_ = kitutil.Unmarshal(item["type"], &kind)
		switch kind {
		case "tool_search_call":
			item["type"] = json.RawMessage(`"function_call"`)
			item["name"], _ = kitutil.Marshal(proxyName)
			if len(item["call_id"]) == 0 {
				item["call_id"] = item["id"]
			}
			if len(item["arguments"]) == 0 {
				item["arguments"] = json.RawMessage(`{}`)
			}
		case "tool_search_output":
			var loaded []json.RawMessage
			if len(item["tools"]) > 0 {
				if err := kitutil.Unmarshal(item["tools"], &loaded); err != nil {
					return nil, nil, fmt.Errorf("invalid tool_search_output tools: %w", err)
				}
			}
			for _, raw := range loaded {
				definition, _, err := normalizeCodexToolDefinition(raw)
				if err != nil {
					return nil, nil, err
				}
				normalized = append(normalized, definition)
			}
			payload, err := kitutil.Marshal(item)
			if err != nil {
				return nil, nil, err
			}
			item["type"] = json.RawMessage(`"function_call_output"`)
			item["output"], _ = kitutil.Marshal(string(payload))
			if len(item["call_id"]) == 0 {
				item["call_id"] = item["id"]
			}
		}
	}
	clone := *source
	clone.Tools, err = kitutil.Marshal(normalized)
	if err != nil {
		return nil, nil, err
	}
	if items != nil {
		clone.Input, err = kitutil.Marshal(items)
		if err != nil {
			return nil, nil, err
		}
	}
	var choice map[string]json.RawMessage
	if kitutil.Unmarshal(source.ToolChoice, &choice) == nil {
		var kind string
		_ = kitutil.Unmarshal(choice["type"], &kind)
		if strings.TrimSpace(kind) == "tool_search" {
			clone.ToolChoice, err = kitutil.Marshal(map[string]string{"type": "function", "name": proxyName})
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return &clone, map[string]struct{}{proxyName: {}}, nil
}

// normalizeCodexToolDefinition accepts legacy carriers without re-encoding schemas.
func normalizeCodexToolDefinition(raw json.RawMessage) (json.RawMessage, bool, error) {
	if kitutil.GetJsonType(raw) == "string" {
		var name string
		if err := kitutil.Unmarshal(raw, &name); err != nil {
			return nil, false, err
		}
		encoded, err := kitutil.Marshal(map[string]string{"type": "custom", "name": name})
		return encoded, true, err
	}
	var value map[string]json.RawMessage
	if err := kitutil.Unmarshal(raw, &value); err != nil {
		return nil, false, err
	}
	var kind string
	_ = kitutil.Unmarshal(value["type"], &kind)
	changed := false
	if kind == "function" && len(value["function"]) > 0 {
		var function map[string]json.RawMessage
		if err := kitutil.Unmarshal(value["function"], &function); err != nil {
			return nil, false, err
		}
		for key, member := range function {
			if len(value[key]) == 0 {
				value[key] = member
			}
		}
		delete(value, "function")
		changed = true
	}
	if kind == "namespace" {
		children := value["tools"]
		if len(children) == 0 && len(value["children"]) > 0 {
			children = value["children"]
			delete(value, "children")
			changed = true
		}
		if len(children) > 0 {
			var members []json.RawMessage
			if err := kitutil.Unmarshal(children, &members); err != nil {
				return nil, false, err
			}
			for i, member := range members {
				normalized, updated, err := normalizeCodexToolDefinition(member)
				if err != nil {
					return nil, false, err
				}
				members[i] = normalized
				changed = changed || updated
			}
			if changed {
				encoded, err := kitutil.Marshal(members)
				if err != nil {
					return nil, false, err
				}
				value["tools"] = encoded
			}
		}
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := kitutil.Marshal(value)
	return encoded, true, err
}
