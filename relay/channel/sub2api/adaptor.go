package sub2api

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/codexcompat"
	"github.com/QuantumNous/new-api/relay/channel/newapi"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	newapi.Adaptor
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

// SetupRequestHeader sets Bearer auth via the embedded newapi adaptor, then
// optionally applies Codex client identity headers when channel setting
// codex_compat_enabled is true.
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.Adaptor.SetupRequestHeader(c, req, info); err != nil {
		return err
	}
	if !codexCompatOn(info) {
		return nil
	}
	if req == nil {
		return nil
	}

	// Compact responses are non-SSE; force JSON Accept regardless of client.
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		req.Set("Accept", "application/json")
	}
	req.Set("Content-Type", "application/json")

	var inbound http.Header
	if c != nil && c.Request != nil {
		inbound = c.Request.Header
	}
	ua := headerLookup(inbound, "User-Agent", "user-agent")
	originator := headerLookup(inbound, "originator", "Originator")
	gate := codexcompat.IsGateCapableOfficialCodex(ua, originator)
	mode := normalizeIdentityMode(info.ChannelSetting.CodexIdentityMode)

	// Align sticky with body prompt_cache_key (design §8.5). Convert runs
	// earlier in the handler path, but header setup must still see the key.
	sticky := resolveStickyFromHeaders(info, inbound, promptCacheKeyFromInfo(info))
	clientName := strings.TrimSpace(info.ChannelSetting.CodexClientName)
	if clientName == "" {
		clientName = codexcompat.DefaultClientName
	}

	codexcompat.ApplyIdentityHeaders(req, codexcompat.ApplyInput{
		Mode:          mode,
		ClientVersion: strings.TrimSpace(info.ChannelSetting.CodexClientVersion),
		ClientName:    clientName,
		Sticky:        sticky,
		Inbound:       inbound,
		GateCapable:   gate,
	})
	return nil
}

// ConvertOpenAIResponsesRequest returns the request unchanged when compat is
// off. When on, injects sticky prompt_cache_key if missing/empty and strips
// empty-string instructions (sub2api codex safety). Does not force store or
// instructions="".
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	out, err := a.Adaptor.ConvertOpenAIResponsesRequest(c, info, request)
	if err != nil {
		return out, err
	}
	if !codexCompatOn(info) {
		return out, nil
	}

	converted, ok := out.(dto.OpenAIResponsesRequest)
	if !ok {
		// Parent always returns the value type; keep a safe path if that changes.
		return out, nil
	}

	var inbound http.Header
	if c != nil && c.Request != nil {
		inbound = c.Request.Header
	}
	pck := rawJSONString(converted.PromptCacheKey)
	sticky := resolveStickyFromHeaders(info, inbound, pck)
	if err := applyCodexCompatBody(&converted, sticky); err != nil {
		return nil, err
	}
	return converted, nil
}

func codexCompatOn(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil && info.ChannelSetting.CodexCompatEnabled
}

func normalizeIdentityMode(mode string) codexcompat.IdentityMode {
	m := codexcompat.IdentityMode(strings.ToLower(strings.TrimSpace(mode)))
	switch m {
	case codexcompat.IdentityModePassthrough, codexcompat.IdentityModeSynthesize:
		return m
	default:
		return codexcompat.IdentityModeAuto
	}
}

func resolveStickyFromHeaders(info *relaycommon.RelayInfo, inbound http.Header, promptCacheKey string) codexcompat.StickyIDs {
	channelID := 0
	userID := 0
	tokenID := 0
	if info != nil {
		channelID = info.GetChannelID()
		userID = info.UserId
		tokenID = info.TokenId
	}
	return codexcompat.ResolveStickyIDs(codexcompat.StickyInput{
		ChannelID:      channelID,
		UserID:         userID,
		TokenID:        tokenID,
		SessionID:      headerLookup(inbound, "session_id", "session-id", "Session-Id"),
		ThreadID:       headerLookup(inbound, "thread_id", "thread-id", "Thread-Id"),
		WindowID:       headerLookup(inbound, "x-codex-window-id", "X-Codex-Window-Id"),
		PromptCacheKey: strings.TrimSpace(promptCacheKey),
		UserAgent:      headerLookup(inbound, "User-Agent", "user-agent"),
		Originator:     headerLookup(inbound, "originator", "Originator"),
	})
}

// applyCodexCompatBody mutates PromptCacheKey / Instructions on the DTO.
func applyCodexCompatBody(request *dto.OpenAIResponsesRequest, sticky codexcompat.StickyIDs) error {
	if request == nil {
		return nil
	}

	// Inject prompt_cache_key when missing or empty string.
	needInject := true
	if len(request.PromptCacheKey) > 0 {
		var s string
		if err := common.Unmarshal(request.PromptCacheKey, &s); err == nil {
			if strings.TrimSpace(s) != "" {
				needInject = false
			}
		} else {
			// Non-string JSON (number/object): leave alone.
			needInject = false
		}
	}
	if needInject {
		key := strings.TrimSpace(sticky.PromptCacheKey)
		if key == "" {
			key = strings.TrimSpace(sticky.ThreadID)
		}
		if key != "" {
			b, err := common.Marshal(key)
			if err != nil {
				return err
			}
			request.PromptCacheKey = b
		}
	}

	// Strip empty-string instructions only; leave omitted / non-empty intact.
	if len(request.Instructions) > 0 {
		var s string
		if err := common.Unmarshal(request.Instructions, &s); err == nil && s == "" {
			request.Instructions = nil
		}
	}
	return nil
}

// promptCacheKeyFromInfo reads body prompt_cache_key from RelayInfo.Request so
// SetupRequestHeader sticky resolution matches Convert (design §8.5).
func promptCacheKeyFromInfo(info *relaycommon.RelayInfo) string {
	if info == nil || info.Request == nil {
		return ""
	}
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		return rawJSONString(req.PromptCacheKey)
	case *dto.OpenAIResponsesCompactionRequest:
		return rawJSONString(req.PromptCacheKey)
	default:
		return ""
	}
}

// rawJSONString returns the unmarshaled string value of raw, or "" if missing / non-string.
func rawJSONString(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := common.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// headerLookup returns the first non-empty value among candidate names, including
// underscore forms that http.Header.Get may not canonicalize.
func headerLookup(h http.Header, names ...string) string {
	if h == nil {
		return ""
	}
	for _, name := range names {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return v
		}
	}
	for _, name := range names {
		want := strings.ToLower(name)
		for k, vals := range h {
			if strings.ToLower(k) != want {
				continue
			}
			for _, v := range vals {
				if t := strings.TrimSpace(v); t != "" {
					return t
				}
			}
		}
	}
	return ""
}
