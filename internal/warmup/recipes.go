// Package warmup implements proactive session-window warmup for OAuth auths.
//
// Some providers (notably Claude Max via OAuth) start a 5-hour session window
// on the first API request. The warmup scheduler fires a minimal request
// against each eligible OAuth auth so operators can align the window with
// working hours or refresh it periodically, instead of accidentally opening
// the window on the first real request of the day.
package warmup

import (
	"strings"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/sjson"
)

// Recipe describes a minimal request payload used to warm up a provider.
//
// The payload is sent in the provider's native format (source == target)
// so the executor translation layer becomes a no-op, keeping warmup
// surface-area independent of translator changes.
type Recipe struct {
	// Provider is the lower-case provider key (e.g. "claude").
	Provider string
	// Model is the upstream model used for warmup (pick the cheapest).
	Model string
	// SourceFormat identifies the payload schema used in Payload.
	SourceFormat sdktranslator.Format
	// Payload is the JSON body in SourceFormat.
	Payload []byte
}

// recipes lists the built-in warmup recipes keyed by lower-case provider.
// Only OAuth-capable providers with a known minimal body are populated.
//
// Payload shape rationale:
//   - input text "ping" instead of "."  — looks like normal greeting traffic
//     rather than a single punctuation mark; reduces risk of Anthropic flagging
//     warmup hits as abnormal patterns.
//   - max_tokens=16 instead of 1  — some providers only start the session
//     window once a non-trivial completion has actually been generated; 16 is
//     still cheap on Haiku/Flash-Lite tiers (sub-cent per round) but gives the
//     model room to produce a real reply rather than an immediate stop.
//
// When adding a new provider, keep the same philosophy: cheapest model, small
// but not degenerate prompt, max-output in the 8–32 range.
var recipes = map[string]Recipe{
	// Claude OAuth (Max plan) — the primary motivation for warmup.
	// "ping" with max_tokens=16 against the cheapest Haiku tier opens the
	// 5-hour session window reliably while keeping cost negligible.
	"claude": {
		Provider:     "claude",
		Model:        "claude-haiku-4-5",
		SourceFormat: sdktranslator.FromString("claude"),
		Payload:      []byte(`{"model":"claude-haiku-4-5","max_tokens":16,"messages":[{"role":"user","content":"ping"}]}`),
	},
	// Codex OAuth (ChatGPT-login). Uses the /v1/responses API.
	"codex": {
		Provider:     "codex",
		Model:        "gpt-5",
		SourceFormat: sdktranslator.FromString("codex"),
		Payload:      []byte(`{"model":"gpt-5","input":"ping","max_output_tokens":16,"store":false}`),
	},
	// Gemini OAuth family — all translate from Gemini native payload.
	// gemini-2.5-flash-lite is the cheapest current generation model.
	"gemini": {
		Provider:     "gemini",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"ping"}]}],"generationConfig":{"maxOutputTokens":16}}`),
	},
	"gemini-cli": {
		Provider:     "gemini-cli",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"ping"}]}],"generationConfig":{"maxOutputTokens":16}}`),
	},
	"aistudio": {
		Provider:     "aistudio",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"ping"}]}],"generationConfig":{"maxOutputTokens":16}}`),
	},
	"vertex": {
		Provider:     "vertex",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"ping"}]}],"generationConfig":{"maxOutputTokens":16}}`),
	},
	// Antigravity uses the Claude payload schema.
	"antigravity": {
		Provider:     "antigravity",
		Model:        "claude-haiku-4-5",
		SourceFormat: sdktranslator.FromString("claude"),
		Payload:      []byte(`{"model":"claude-haiku-4-5","max_tokens":16,"messages":[{"role":"user","content":"ping"}]}`),
	},
	// Kimi — OAuth-backed but rarely has strict session windows. Kept here
	// so future operators can opt in; executor uses OpenAI format.
	"kimi": {
		Provider:     "kimi",
		Model:        "kimi-k2-turbo-preview",
		SourceFormat: sdktranslator.FromString("openai"),
		Payload:      []byte(`{"model":"kimi-k2-turbo-preview","max_tokens":16,"messages":[{"role":"user","content":"ping"}]}`),
	},
}

// lookupRecipe returns the recipe for a provider key, case-insensitive.
func lookupRecipe(provider string) (Recipe, bool) {
	r, ok := recipes[strings.ToLower(strings.TrimSpace(provider))]
	return r, ok
}

// SupportedProviders returns the set of provider keys that have a warmup
// recipe registered. The list is not sorted.
func SupportedProviders() []string {
	out := make([]string, 0, len(recipes))
	for k := range recipes {
		out = append(out, k)
	}
	return out
}

// overrideModelInPayload returns a copy of the recipe payload with the top
// level "model" field replaced. For Gemini-family recipes the payload does
// not carry a top-level model — those executors pick up the model from
// Request.Model, so the original payload is returned unchanged.
func overrideModelInPayload(r Recipe, model string) []byte {
	format := r.SourceFormat.String()
	switch format {
	case "claude", "codex", "openai":
		out, err := sjson.SetBytes(append([]byte(nil), r.Payload...), "model", model)
		if err != nil {
			return r.Payload
		}
		return out
	default:
		return r.Payload
	}
}
