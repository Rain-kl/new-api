package model

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Temporary hop cooldown for model-redirect HA.
//
// On each hop-unavailability failure of a (channel_id, attempt_model) pair,
// disable that hop for failCount * step (default 1 minute), capped at max
// (default 30 minutes). A successful attempt clears the hop state.
//
// Process-local only (no Redis): each replica keeps independent state.
// Recording a failure only affects subsequent requests' selection filters;
// the current request keeps walking its already-built candidate list.
//
// Hop identity must match FilterRedirectCooldownDown / SetupContext
// original_model (attempt model at hop start), not post-handler rewrites of
// RelayInfo.OriginModelName.

const (
	modelRedirectCooldownStepDefault = time.Minute
	modelRedirectCooldownMaxDefault  = 30 * time.Minute
)

// Overridable in tests.
var (
	modelRedirectCooldownStep = modelRedirectCooldownStepDefault
	modelRedirectCooldownMax  = modelRedirectCooldownMaxDefault
	modelRedirectNow          = time.Now
)

type modelRedirectHopCooldown struct {
	failCount     int
	disabledUntil time.Time
}

var (
	modelRedirectCooldownMu sync.RWMutex
	modelRedirectCooldowns   = map[string]*modelRedirectHopCooldown{}
)

func modelRedirectHopKey(channelID int, attemptModel string) string {
	return fmt.Sprintf("%d\x00%s", channelID, strings.TrimSpace(attemptModel))
}

// IsModelRedirectHopDisabled reports whether the hop is still in cooldown.
func IsModelRedirectHopDisabled(channelID int, attemptModel string) bool {
	if channelID <= 0 {
		return false
	}
	key := modelRedirectHopKey(channelID, attemptModel)
	now := modelRedirectNow()
	modelRedirectCooldownMu.RLock()
	st, ok := modelRedirectCooldowns[key]
	var until time.Time
	if ok && st != nil {
		until = st.disabledUntil
	}
	modelRedirectCooldownMu.RUnlock()
	if !ok || st == nil {
		return false
	}
	return now.Before(until)
}

// RecordModelRedirectHopFailure increments fail count and extends cooldown:
// duration = min(failCount * step, max). Affects subsequent requests only.
func RecordModelRedirectHopFailure(channelID int, attemptModel string) {
	if channelID <= 0 {
		return
	}
	key := modelRedirectHopKey(channelID, attemptModel)
	now := modelRedirectNow()

	modelRedirectCooldownMu.Lock()
	defer modelRedirectCooldownMu.Unlock()
	st := modelRedirectCooldowns[key]
	if st == nil {
		st = &modelRedirectHopCooldown{}
		modelRedirectCooldowns[key] = st
	}
	st.failCount++
	// Cap duration steps to avoid pathological Duration overflow; keep failCount for logs.
	steps := st.failCount
	maxSteps := int(modelRedirectCooldownMax / modelRedirectCooldownStep)
	if maxSteps < 1 {
		maxSteps = 1
	}
	if steps > maxSteps {
		steps = maxSteps
	}
	d := time.Duration(steps) * modelRedirectCooldownStep
	if d > modelRedirectCooldownMax {
		d = modelRedirectCooldownMax
	}
	st.disabledUntil = now.Add(d)
	common.SysLog(fmt.Sprintf(
		"model redirect hop cooldown: channel=%d model=%q fails=%d until=%s (duration=%s)",
		channelID, strings.TrimSpace(attemptModel), st.failCount,
		st.disabledUntil.Format(time.RFC3339), d,
	))
}

// ClearModelRedirectHopCooldown resets fail count after a successful hop.
func ClearModelRedirectHopCooldown(channelID int, attemptModel string) {
	if channelID <= 0 {
		return
	}
	key := modelRedirectHopKey(channelID, attemptModel)
	modelRedirectCooldownMu.Lock()
	delete(modelRedirectCooldowns, key)
	modelRedirectCooldownMu.Unlock()
}

// FilterRedirectCooldownDown drops hops that are temporarily disabled.
// clientModel is used with AttemptModel for empty-model candidates.
func FilterRedirectCooldownDown(cands []RedirectCandidate, clientModel string) []RedirectCandidate {
	if len(cands) == 0 {
		return nil
	}
	out := make([]RedirectCandidate, 0, len(cands))
	for _, cand := range cands {
		if cand.ChannelID <= 0 {
			continue
		}
		attemptModel := AttemptModel(clientModel, cand)
		if IsModelRedirectHopDisabled(cand.ChannelID, attemptModel) {
			continue
		}
		out = append(out, cand)
	}
	return out
}

// resetModelRedirectCooldownsForTest clears all hop cooldowns (tests only).
func resetModelRedirectCooldownsForTest() {
	modelRedirectCooldownMu.Lock()
	modelRedirectCooldowns = map[string]*modelRedirectHopCooldown{}
	modelRedirectCooldownMu.Unlock()
}
