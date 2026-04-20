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
// When adding a new provider, pick the cheapest available model and set
// max_tokens / maxOutputTokens to 1 so the warmup has negligible cost.
var recipes = map[string]Recipe{
	// Claude OAuth (Max plan) — the primary motivation for warmup.
	// A single-byte user message with max_tokens=1 against the cheapest
	// Haiku tier is sufficient to open the 5-hour session window.
	"claude": {
		Provider:     "claude",
		Model:        "claude-haiku-4-5",
		SourceFormat: sdktranslator.FromString("claude"),
		Payload:      []byte(`{"model":"claude-haiku-4-5","max_tokens":1,"messages":[{"role":"user","content":"."}]}`),
	},
	// Codex OAuth (ChatGPT-login). Uses the /v1/responses API under
	// Anthropic's lightest Codex model.
	"codex": {
		Provider:     "codex",
		Model:        "gpt-5",
		SourceFormat: sdktranslator.FromString("codex"),
		Payload:      []byte(`{"model":"gpt-5","input":".","max_output_tokens":16,"store":false}`),
	},
	// Gemini OAuth family — all translate from Gemini native payload.
	// gemini-2.5-flash-lite is the cheapest current generation model.
	"gemini": {
		Provider:     "gemini",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"."}]}],"generationConfig":{"maxOutputTokens":1}}`),
	},
	"gemini-cli": {
		Provider:     "gemini-cli",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"."}]}],"generationConfig":{"maxOutputTokens":1}}`),
	},
	"aistudio": {
		Provider:     "aistudio",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"."}]}],"generationConfig":{"maxOutputTokens":1}}`),
	},
	"vertex": {
		Provider:     "vertex",
		Model:        "gemini-2.5-flash-lite",
		SourceFormat: sdktranslator.FromString("gemini"),
		Payload:      []byte(`{"contents":[{"role":"user","parts":[{"text":"."}]}],"generationConfig":{"maxOutputTokens":1}}`),
	},
	// Antigravity uses the Claude payload schema.
	"antigravity": {
		Provider:     "antigravity",
		Model:        "claude-haiku-4-5",
		SourceFormat: sdktranslator.FromString("claude"),
		Payload:      []byte(`{"model":"claude-haiku-4-5","max_tokens":1,"messages":[{"role":"user","content":"."}]}`),
	},
	// Kimi — OAuth-backed but rarely has strict session windows. Kept here
	// so future operators can opt in; executor uses OpenAI format.
	"kimi": {
		Provider:     "kimi",
		Model:        "kimi-k2-turbo-preview",
		SourceFormat: sdktranslator.FromString("openai"),
		Payload:      []byte(`{"model":"kimi-k2-turbo-preview","max_tokens":1,"messages":[{"role":"user","content":"."}]}`),
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
