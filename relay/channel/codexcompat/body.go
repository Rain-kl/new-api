package codexcompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// PatchResponsesBodyJSON injects sticky prompt_cache_key when missing/empty and
// optionally removes empty-string "instructions" (sub2api codex safety).
// body must be a JSON object.
func PatchResponsesBodyJSON(body []byte, sticky StickyIDs, stripEmptyInstructions bool) ([]byte, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("codexcompat: empty body")
	}
	// Decode into ordered-friendly map of raw values so we only touch known keys.
	var obj map[string]any
	if err := common.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("codexcompat: body json: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("codexcompat: body is not a json object")
	}

	// Inject prompt_cache_key when missing or empty string.
	needInject := true
	if raw, ok := obj["prompt_cache_key"]; ok {
		switch v := raw.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				needInject = false
			}
		case nil:
			// treat as missing
		default:
			// Non-string non-nil (e.g. number) — leave alone.
			needInject = false
		}
	}
	if needInject {
		key := strings.TrimSpace(sticky.PromptCacheKey)
		if key == "" {
			key = strings.TrimSpace(sticky.ThreadID)
		}
		if key != "" {
			obj["prompt_cache_key"] = key
		}
	}

	if stripEmptyInstructions {
		if raw, ok := obj["instructions"]; ok {
			if s, isStr := raw.(string); isStr && s == "" {
				delete(obj, "instructions")
			}
		}
	}

	out, err := common.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("codexcompat: marshal body: %w", err)
	}
	return out, nil
}
