package config

import "testing"

func TestSanitizeOpenAICompatibilityNormalizesProtocolAndAuthType(t *testing.T) {
	cfg := &Config{OpenAICompatibility: []OpenAICompatibility{
		{BaseURL: " https://provider.example/v1 ", Protocol: " Anthropic ", AuthType: "APIKEY"},
		{BaseURL: "https://legacy.example/v1", Protocol: "unknown", AuthType: "unknown"},
	}}

	cfg.SanitizeOpenAICompatibility()
	if got := cfg.OpenAICompatibility[0].Protocol; got != "anthropic" {
		t.Fatalf("protocol = %q, want anthropic", got)
	}
	if got := cfg.OpenAICompatibility[0].AuthType; got != "x-api-key" {
		t.Fatalf("auth type = %q, want x-api-key", got)
	}
	if got := cfg.OpenAICompatibility[1].Protocol; got != "openai" {
		t.Fatalf("legacy protocol = %q, want openai", got)
	}
	if got := cfg.OpenAICompatibility[1].AuthType; got != "bearer" {
		t.Fatalf("legacy auth type = %q, want bearer", got)
	}
}
