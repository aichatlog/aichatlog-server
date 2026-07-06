// Package pricing turns token counts into list-price cost estimates for Claude
// models. Rates are best-effort public list prices and MUST be treated as
// estimates, not billed amounts: subscription plans (Pro/Max/Team) include
// usage, so the numbers below are what the same tokens would cost on the
// pay-as-you-go API, not what you are actually charged.
package pricing

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// Cache multipliers apply to a model's INPUT rate (Anthropic prompt caching).
const (
	CacheWrite5mMult = 1.25 // 5-minute cache write
	CacheWrite1hMult = 2.0  // 1-hour cache write (captured in Phase 2)
	CacheReadMult    = 0.10 // cache read (hit)
)

// ModelRate is the per-million-token list price for a model family, in USD.
type ModelRate struct {
	Input  float64 `json:"input"`  // USD per 1M input tokens
	Output float64 `json:"output"` // USD per 1M output tokens
}

// defaultRates are keyed by model family (matched as a substring of the model
// id). List prices per 1M tokens, standard tier. Keep current via the
// AICHATLOG_PRICING env override; Anthropic changes these periodically.
var defaultRates = map[string]ModelRate{
	"opus":   {Input: 5, Output: 25},
	"sonnet": {Input: 3, Output: 15},
	"haiku":  {Input: 1, Output: 5},
}

var (
	ratesOnce sync.Once
	rates     map[string]ModelRate
)

// load resolves the active rate table: defaults overlaid with the optional
// AICHATLOG_PRICING env var, a JSON object like
// {"opus":{"input":5,"output":25},"sonnet":{"input":3,"output":15}}. Parsed once.
func load() {
	rates = make(map[string]ModelRate, len(defaultRates))
	for k, v := range defaultRates {
		rates[k] = v
	}
	if raw := strings.TrimSpace(os.Getenv("AICHATLOG_PRICING")); raw != "" {
		// Merge onto existing rates so a partial override (e.g. only "input")
		// does not zero the field the user omitted. Pointers detect presence.
		var override map[string]struct {
			Input  *float64 `json:"input"`
			Output *float64 `json:"output"`
		}
		if err := json.Unmarshal([]byte(raw), &override); err == nil {
			for k, v := range override {
				k = strings.ToLower(k)
				r := rates[k] // existing default, or zero value for a new family
				if v.Input != nil {
					r.Input = *v.Input
				}
				if v.Output != nil {
					r.Output = *v.Output
				}
				rates[k] = r
			}
		}
	}
}

// Rates returns a copy of the active model-family rate table.
func Rates() map[string]ModelRate {
	ratesOnce.Do(load)
	out := make(map[string]ModelRate, len(rates))
	for k, v := range rates {
		out[k] = v
	}
	return out
}

// Family maps a model id (e.g. "claude-opus-4-8") to a known rate family key,
// or "" if unrecognized.
func Family(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return "opus"
	case strings.Contains(m, "sonnet"):
		return "sonnet"
	case strings.Contains(m, "haiku"):
		return "haiku"
	default:
		return ""
	}
}

// Cost estimates the USD list price for token counts attributed to one model,
// with the 5-minute vs 1-hour cache-write split priced separately. Returns
// (cost, priced); priced is false when the model family is unknown, in which
// case cost is 0 and the caller should treat those tokens as unpriced rather
// than free.
func Cost(model string, input, output, cacheRead, cacheCreation5m, cacheCreation1h int64) (float64, bool) {
	fam := Family(model)
	if fam == "" {
		return 0, false
	}
	ratesOnce.Do(load)
	r, ok := rates[fam]
	if !ok {
		return 0, false
	}
	const perMillion = 1_000_000.0
	cost := float64(input)*r.Input +
		float64(output)*r.Output +
		float64(cacheCreation5m)*r.Input*CacheWrite5mMult +
		float64(cacheCreation1h)*r.Input*CacheWrite1hMult +
		float64(cacheRead)*r.Input*CacheReadMult
	return cost / perMillion, true
}
