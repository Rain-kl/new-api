package codexcompat_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/codexcompat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchResponsesBodyJSON_InjectsPromptCacheKeyWhenMissing(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{
		ThreadID:       "thread-1",
		PromptCacheKey: "thread-1",
	}
	out, err := codexcompat.PatchResponsesBodyJSON(
		[]byte(`{"model":"gpt-5","input":"hi"}`),
		sticky,
		false,
	)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, common.Unmarshal(out, &m))
	assert.Equal(t, "thread-1", m["prompt_cache_key"])
	assert.Equal(t, "gpt-5", m["model"])
	assert.Equal(t, "hi", m["input"])
}

func TestPatchResponsesBodyJSON_InjectsWhenEmptyString(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{PromptCacheKey: "from-sticky"}
	out, err := codexcompat.PatchResponsesBodyJSON(
		[]byte(`{"model":"m","prompt_cache_key":""}`),
		sticky,
		false,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, common.Unmarshal(out, &m))
	assert.Equal(t, "from-sticky", m["prompt_cache_key"])
}

func TestPatchResponsesBodyJSON_PreservesExistingPromptCacheKey(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{PromptCacheKey: "sticky-key"}
	out, err := codexcompat.PatchResponsesBodyJSON(
		[]byte(`{"model":"m","prompt_cache_key":"client-key"}`),
		sticky,
		false,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, common.Unmarshal(out, &m))
	assert.Equal(t, "client-key", m["prompt_cache_key"])
}

func TestPatchResponsesBodyJSON_StripsEmptyInstructions(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{PromptCacheKey: "k"}
	out, err := codexcompat.PatchResponsesBodyJSON(
		[]byte(`{"model":"m","instructions":"","input":"x"}`),
		sticky,
		true,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, common.Unmarshal(out, &m))
	_, has := m["instructions"]
	assert.False(t, has, "empty instructions should be stripped")
	assert.Equal(t, "k", m["prompt_cache_key"])
}

func TestPatchResponsesBodyJSON_LeavesNonEmptyInstructions(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{PromptCacheKey: "k"}
	out, err := codexcompat.PatchResponsesBodyJSON(
		[]byte(`{"model":"m","instructions":"be helpful","input":"x"}`),
		sticky,
		true,
	)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, common.Unmarshal(out, &m))
	assert.Equal(t, "be helpful", m["instructions"])
}

func TestPatchResponsesBodyJSON_EmptyBody(t *testing.T) {
	t.Parallel()
	_, err := codexcompat.PatchResponsesBodyJSON(nil, codexcompat.StickyIDs{PromptCacheKey: "k"}, true)
	assert.Error(t, err)
	_, err = codexcompat.PatchResponsesBodyJSON([]byte("[]"), codexcompat.StickyIDs{PromptCacheKey: "k"}, true)
	assert.Error(t, err)
}
