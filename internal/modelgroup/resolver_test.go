package modelgroup

import (
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func makeGroup(name string, entries ...config.ModelGroupEntry) *config.ModelGroup {
	return &config.ModelGroup{Name: name, Models: entries}
}

func entry(model string, priority int) config.ModelGroupEntry {
	return config.ModelGroupEntry{Model: model, Priority: priority}
}

func TestGroupByPriority_SingleTier(t *testing.T) {
	entries := []config.ModelGroupEntry{
		entry("a", 1),
		entry("b", 1),
	}
	tiers := GroupByPriority(entries)
	if len(tiers) != 1 {
		t.Fatalf("expected 1 tier, got %d", len(tiers))
	}
	if len(tiers[0].Models) != 2 {
		t.Errorf("expected 2 models in tier, got %d", len(tiers[0].Models))
	}
	if tiers[0].Priority != 1 {
		t.Errorf("expected priority 1, got %d", tiers[0].Priority)
	}
}

func TestGroupByPriority_MultipleTiers(t *testing.T) {
	entries := []config.ModelGroupEntry{
		entry("a", 1),
		entry("b", 1),
		entry("c", 2),
		entry("d", 3),
	}
	tiers := GroupByPriority(entries)
	if len(tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(tiers))
	}
	// Highest priority first.
	if tiers[0].Priority != 3 {
		t.Errorf("expected first tier priority 3, got %d", tiers[0].Priority)
	}
	if tiers[1].Priority != 2 {
		t.Errorf("expected second tier priority 2, got %d", tiers[1].Priority)
	}
	if tiers[2].Priority != 1 {
		t.Errorf("expected third tier priority 1, got %d", tiers[2].Priority)
	}
	if len(tiers[2].Models) != 2 {
		t.Errorf("expected 2 models in priority-1 tier, got %d", len(tiers[2].Models))
	}
}

func TestGroupByPriority_Empty(t *testing.T) {
	tiers := GroupByPriority(nil)
	if len(tiers) != 0 {
		t.Errorf("expected 0 tiers for nil, got %d", len(tiers))
	}
}

func TestGroupByPriority_StableOrder(t *testing.T) {
	/*
	 * Models within a tier must appear in the order they were defined in config,
	 * so that deterministic round-robin ordering is preserved.
	 */
	entries := []config.ModelGroupEntry{
		entry("z-model", 1),
		entry("a-model", 1),
	}
	tiers := GroupByPriority(entries)
	if tiers[0].Models[0] != "z-model" || tiers[0].Models[1] != "a-model" {
		t.Errorf("expected insertion order preserved, got %v", tiers[0].Models)
	}
}

func TestCheckModelAccess_NilConfig_AllowsAll(t *testing.T) {
	err := CheckModelAccess(nil, "any-model")
	if err != nil {
		t.Errorf("expected nil error for nil config, got %v", err)
	}
}

func TestCheckModelAccess_NoRestrictions_AllowsAll(t *testing.T) {
	kc := &config.APIKeyConfig{Key: "k"}
	err := CheckModelAccess(kc, "any-model")
	if err != nil {
		t.Errorf("expected nil error for unrestricted key, got %v", err)
	}
}

func TestCheckModelAccess_AllowedModels_AllowsMatchingModel(t *testing.T) {
	kc := &config.APIKeyConfig{
		Key:           "k",
		AllowedModels: []string{"claude-sonnet-4", "gemini-pro"},
	}
	if err := CheckModelAccess(kc, "claude-sonnet-4"); err != nil {
		t.Errorf("expected allowed model to pass, got %v", err)
	}
}

func TestCheckModelAccess_AllowedModels_RejectsUnknownModel(t *testing.T) {
	kc := &config.APIKeyConfig{
		Key:           "k",
		AllowedModels: []string{"claude-sonnet-4"},
	}
	err := CheckModelAccess(kc, "gpt-4")
	if err == nil {
		t.Fatal("expected error for disallowed model")
	}
	var ae *AccessDeniedError
	if !errors.As(err, &ae) {
		t.Errorf("expected AccessDeniedError, got %T: %v", err, err)
	}
	if ae.StatusCode() != 403 {
		t.Errorf("expected status 403, got %d", ae.StatusCode())
	}
}

func TestCheckModelAccess_ModelGroup_AllowsGroupName(t *testing.T) {
	kc := &config.APIKeyConfig{
		Key:        "k",
		ModelGroup: "my-group",
	}
	// When a key has a ModelGroup, the group name itself is a valid "model" to request.
	err := CheckModelAccess(kc, "my-group")
	if err != nil {
		t.Errorf("expected group name to be allowed, got %v", err)
	}
}

func TestCheckModelAccess_ModelGroup_RejectsModelOutsideGroup(t *testing.T) {
	/*
	 * When AllowedModels is empty but ModelGroup is set, the key may only be
	 * used with the group name as model. Direct model requests are rejected.
	 */
	kc := &config.APIKeyConfig{
		Key:        "k",
		ModelGroup: "my-group",
	}
	err := CheckModelAccess(kc, "some-other-model")
	if err == nil {
		t.Fatal("expected error when model is not in group and no AllowedModels")
	}
	var ae *AccessDeniedError
	if !errors.As(err, &ae) {
		t.Errorf("expected AccessDeniedError, got %T", err)
	}
}

func TestCheckModelAccess_AllowedModels_WithModelGroup_AllowsBoth(t *testing.T) {
	/*
	 * When both AllowedModels and ModelGroup are set, the key can access
	 * explicit allowed models AND the group name.
	 */
	kc := &config.APIKeyConfig{
		Key:           "k",
		AllowedModels: []string{"claude-sonnet-4"},
		ModelGroup:    "my-group",
	}
	if err := CheckModelAccess(kc, "claude-sonnet-4"); err != nil {
		t.Errorf("expected allowed model to pass, got %v", err)
	}
	if err := CheckModelAccess(kc, "my-group"); err != nil {
		t.Errorf("expected group name to pass, got %v", err)
	}
}

func TestCheckModelAccess_AllowedModels_WithModelGroup_RejectsOther(t *testing.T) {
	kc := &config.APIKeyConfig{
		Key:           "k",
		AllowedModels: []string{"claude-sonnet-4"},
		ModelGroup:    "my-group",
	}
	err := CheckModelAccess(kc, "gpt-4")
	if err == nil {
		t.Fatal("expected error for model not in allowed list or group")
	}
}

func TestIsGroupModel_MatchesGroupName(t *testing.T) {
	g := makeGroup("claude-failover", entry("a", 1))
	if !IsGroupModel("claude-failover", g) {
		t.Error("expected group name to match")
	}
}

func TestIsGroupModel_NilGroup(t *testing.T) {
	if IsGroupModel("name", nil) {
		t.Error("expected false for nil group")
	}
}

func TestIsGroupModel_EmptyName(t *testing.T) {
	g := makeGroup("g")
	if IsGroupModel("", g) {
		t.Error("expected false for empty name")
	}
}

func TestGroupByPriority_NegativePriority(t *testing.T) {
	entries := []config.ModelGroupEntry{
		entry("high", 5),
		entry("low", -1),
	}
	tiers := GroupByPriority(entries)
	if len(tiers) != 2 {
		t.Fatalf("expected 2 tiers, got %d", len(tiers))
	}
	if tiers[0].Priority != 5 {
		t.Errorf("expected highest priority 5 first, got %d", tiers[0].Priority)
	}
}

func TestAccessDeniedError_Message(t *testing.T) {
	ae := &AccessDeniedError{Model: "gpt-4", Key: "k"}
	msg := ae.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}
