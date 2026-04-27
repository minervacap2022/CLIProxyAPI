package warmup

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestOverrideModelInPayload_Claude(t *testing.T) {
	r := recipes["claude"]
	out := overrideModelInPayload(r, "claude-sonnet-4-6")
	got := gjson.GetBytes(out, "model").String()
	if got != "claude-sonnet-4-6" {
		t.Errorf("claude payload model = %q, want claude-sonnet-4-6", got)
	}
	// Ensure the original recipe payload is not mutated.
	orig := gjson.GetBytes(r.Payload, "model").String()
	if orig == "claude-sonnet-4-6" {
		t.Error("recipe.Payload was mutated by overrideModelInPayload")
	}
}

func TestOverrideModelInPayload_Codex(t *testing.T) {
	r := recipes["codex"]
	out := overrideModelInPayload(r, "gpt-5-nano")
	got := gjson.GetBytes(out, "model").String()
	if got != "gpt-5-nano" {
		t.Errorf("codex payload model = %q, want gpt-5-nano", got)
	}
}

func TestOverrideModelInPayload_GeminiPayloadUnchanged(t *testing.T) {
	// Gemini-format payload has no top-level "model" — Request.Model carries it.
	// overrideModelInPayload should return the payload unchanged.
	r := recipes["gemini"]
	out := overrideModelInPayload(r, "gemini-2.5-flash")
	if string(out) != string(r.Payload) {
		t.Errorf("gemini payload should be unchanged, got %s", string(out))
	}
}

func TestSupportedProvidersCoversAllRecipes(t *testing.T) {
	if got, want := len(SupportedProviders()), len(recipes); got != want {
		t.Errorf("SupportedProviders returned %d, recipes has %d", got, want)
	}
}

func TestLookupRecipe_CaseInsensitive(t *testing.T) {
	if _, ok := lookupRecipe("CLAUDE"); !ok {
		t.Error("lookupRecipe should be case-insensitive")
	}
	if _, ok := lookupRecipe(" gemini "); !ok {
		t.Error("lookupRecipe should trim whitespace")
	}
}
