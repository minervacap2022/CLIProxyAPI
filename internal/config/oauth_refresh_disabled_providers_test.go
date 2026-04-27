package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfig_ParsesOAuthRefreshDisabledProviders(t *testing.T) {
	raw := []byte(`
oauth-refresh-disabled-providers:
  - Claude
  - kimi
`)

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if len(cfg.OAuthRefreshDisabledProviders) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(cfg.OAuthRefreshDisabledProviders))
	}
	if cfg.OAuthRefreshDisabledProviders[0] != "Claude" {
		t.Fatalf("first provider = %q, want Claude", cfg.OAuthRefreshDisabledProviders[0])
	}
	if cfg.OAuthRefreshDisabledProviders[1] != "kimi" {
		t.Fatalf("second provider = %q, want kimi", cfg.OAuthRefreshDisabledProviders[1])
	}
}
