package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, yamlData string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(yamlData), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return p
}

func TestMergeAPIKeyConfigsIntoFlatList_EmptyConfigs(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{APIKeys: []string{"existing-key"}},
	}
	cfg.MergeAPIKeyConfigsIntoFlatList()
	if len(cfg.APIKeys) != 1 || cfg.APIKeys[0] != "existing-key" {
		t.Fatalf("expected flat list unchanged, got %v", cfg.APIKeys)
	}
}

func TestMergeAPIKeyConfigsIntoFlatList_AddsNewKeys(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{APIKeys: []string{"key-a"}},
		APIKeyConfigs: []APIKeyConfig{
			{Key: "key-b", Label: "Team B"},
			{Key: "key-c"},
		},
	}
	cfg.MergeAPIKeyConfigsIntoFlatList()
	if len(cfg.APIKeys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(cfg.APIKeys), cfg.APIKeys)
	}
}

func TestMergeAPIKeyConfigsIntoFlatList_DeduplicatesExisting(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{APIKeys: []string{"key-a", "key-b"}},
		APIKeyConfigs: []APIKeyConfig{
			{Key: "key-b", Label: "already in flat"},
			{Key: "key-c"},
		},
	}
	cfg.MergeAPIKeyConfigsIntoFlatList()
	if len(cfg.APIKeys) != 3 {
		t.Fatalf("expected 3 keys (no dup), got %d: %v", len(cfg.APIKeys), cfg.APIKeys)
	}
}

func TestMergeAPIKeyConfigsIntoFlatList_SkipsBlankKeys(t *testing.T) {
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{Key: "  ", Label: "blank key"},
			{Key: "valid-key"},
		},
	}
	cfg.MergeAPIKeyConfigsIntoFlatList()
	if len(cfg.APIKeys) != 1 || cfg.APIKeys[0] != "valid-key" {
		t.Fatalf("expected only 'valid-key', got %v", cfg.APIKeys)
	}
}

func TestMergeAPIKeyConfigsIntoFlatList_Idempotent(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{APIKeys: []string{"key-a"}},
		APIKeyConfigs: []APIKeyConfig{
			{Key: "key-b"},
		},
	}
	cfg.MergeAPIKeyConfigsIntoFlatList()
	first := append([]string(nil), cfg.APIKeys...)
	cfg.MergeAPIKeyConfigsIntoFlatList()
	if len(cfg.APIKeys) != len(first) {
		t.Errorf("merge is not idempotent: first=%v second=%v", first, cfg.APIKeys)
	}
	for i, k := range first {
		if cfg.APIKeys[i] != k {
			t.Errorf("key mismatch at %d: %q vs %q", i, k, cfg.APIKeys[i])
		}
	}
}

func TestMergeAPIKeyConfigsIntoFlatList_NilReceiver(t *testing.T) {
	var cfg *Config
	cfg.MergeAPIKeyConfigsIntoFlatList() // must not panic
}

func TestSanitizeAPIKeyConfigs_TrimsWhitespace(t *testing.T) {
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{Key: "  key-a  ", Label: "  Team A  ", ModelGroup: "  group-1  "},
		},
	}
	cfg.SanitizeAPIKeyConfigs()
	kc := cfg.APIKeyConfigs[0]
	if kc.Key != "key-a" {
		t.Errorf("expected trimmed key 'key-a', got %q", kc.Key)
	}
	if kc.Label != "Team A" {
		t.Errorf("expected trimmed label 'Team A', got %q", kc.Label)
	}
	if kc.ModelGroup != "group-1" {
		t.Errorf("expected trimmed model group 'group-1', got %q", kc.ModelGroup)
	}
}

func TestSanitizeAPIKeyConfigs_DropsBlankKeys(t *testing.T) {
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{Key: ""},
			{Key: "  "},
			{Key: "valid"},
		},
	}
	cfg.SanitizeAPIKeyConfigs()
	if len(cfg.APIKeyConfigs) != 1 || cfg.APIKeyConfigs[0].Key != "valid" {
		t.Fatalf("expected 1 valid entry, got %v", cfg.APIKeyConfigs)
	}
}

func TestSanitizeAPIKeyConfigs_TrimsAllowedModels(t *testing.T) {
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{
				Key:           "k",
				AllowedModels: []string{"  claude-sonnet  ", "", "  gemini-pro  "},
			},
		},
	}
	cfg.SanitizeAPIKeyConfigs()
	models := cfg.APIKeyConfigs[0].AllowedModels
	if len(models) != 2 {
		t.Fatalf("expected 2 trimmed models, got %v", models)
	}
	if models[0] != "claude-sonnet" || models[1] != "gemini-pro" {
		t.Errorf("unexpected model values: %v", models)
	}
}

func TestSanitizeAPIKeyConfigs_ModelGroupAndAllowedModelsCoexist(t *testing.T) {
	/*
	 * Both AllowedModels and ModelGroup may be set simultaneously:
	 * AllowedModels acts as explicit whitelist, ModelGroup provides failover routing.
	 * Sanitization must preserve both fields.
	 */
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{
				Key:           "k",
				AllowedModels: []string{"model-a"},
				ModelGroup:    "my-group",
			},
		},
	}
	cfg.SanitizeAPIKeyConfigs()
	kc := cfg.APIKeyConfigs[0]
	if len(kc.AllowedModels) != 1 {
		t.Errorf("expected AllowedModels to remain, got %v", kc.AllowedModels)
	}
	if kc.ModelGroup != "my-group" {
		t.Errorf("expected ModelGroup to remain, got %q", kc.ModelGroup)
	}
}

func TestSanitizeAPIKeyConfigs_NilReceiver(t *testing.T) {
	var cfg *Config
	cfg.SanitizeAPIKeyConfigs() // must not panic
}

func TestSanitizeModelGroups_TrimsWhitespace(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroup{
			{
				Name: "  group-1  ",
				Models: []ModelGroupEntry{
					{Model: "  claude-sonnet  ", Priority: 3},
					{Model: "  claude-haiku  ", Priority: 1},
				},
			},
		},
	}
	cfg.SanitizeModelGroups()
	g := cfg.ModelGroups[0]
	if g.Name != "group-1" {
		t.Errorf("expected trimmed name 'group-1', got %q", g.Name)
	}
	if g.Models[0].Model != "claude-sonnet" {
		t.Errorf("expected trimmed model 'claude-sonnet', got %q", g.Models[0].Model)
	}
}

func TestSanitizeModelGroups_DropsGroupsWithBlankName(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroup{
			{Name: ""},
			{Name: "  "},
			{Name: "valid-group", Models: []ModelGroupEntry{{Model: "m1", Priority: 1}}},
		},
	}
	cfg.SanitizeModelGroups()
	if len(cfg.ModelGroups) != 1 || cfg.ModelGroups[0].Name != "valid-group" {
		t.Fatalf("expected 1 valid group, got %v", cfg.ModelGroups)
	}
}

func TestSanitizeModelGroups_DropsEntriesWithBlankModel(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroup{
			{
				Name: "g",
				Models: []ModelGroupEntry{
					{Model: "", Priority: 1},
					{Model: "  ", Priority: 2},
					{Model: "real-model", Priority: 3},
				},
			},
		},
	}
	cfg.SanitizeModelGroups()
	models := cfg.ModelGroups[0].Models
	if len(models) != 1 || models[0].Model != "real-model" {
		t.Fatalf("expected 1 valid model entry, got %v", models)
	}
}

func TestSanitizeModelGroups_RemovesGroupWithNoModels(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroup{
			{Name: "empty-group", Models: nil},
			{Name: "valid-group", Models: []ModelGroupEntry{{Model: "m1", Priority: 1}}},
		},
	}
	cfg.SanitizeModelGroups()
	if len(cfg.ModelGroups) != 1 || cfg.ModelGroups[0].Name != "valid-group" {
		t.Fatalf("expected only valid-group to remain, got %v", cfg.ModelGroups)
	}
}

func TestSanitizeModelGroups_CasePreserved(t *testing.T) {
	/*
	 * Group names are case-sensitive identifiers; sanitization must not normalize case.
	 */
	cfg := &Config{
		ModelGroups: []ModelGroup{
			{Name: "MyGroup", Models: []ModelGroupEntry{{Model: "m1", Priority: 1}}},
		},
	}
	cfg.SanitizeModelGroups()
	if cfg.ModelGroups[0].Name != "MyGroup" {
		t.Errorf("expected case preserved 'MyGroup', got %q", cfg.ModelGroups[0].Name)
	}
}

func TestSanitizeModelGroups_NilReceiver(t *testing.T) {
	var cfg *Config
	cfg.SanitizeModelGroups() // must not panic
}

func TestLookupAPIKeyConfig_FindsByKey(t *testing.T) {
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{Key: "key-a", Label: "A"},
			{Key: "key-b", Label: "B"},
		},
	}
	result := cfg.LookupAPIKeyConfig("key-b")
	if result == nil || result.Label != "B" {
		t.Fatalf("expected to find key-b with label B, got %v", result)
	}
}

func TestLookupAPIKeyConfig_ReturnsNilForUnknown(t *testing.T) {
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{Key: "key-a"},
		},
	}
	if cfg.LookupAPIKeyConfig("nonexistent") != nil {
		t.Fatal("expected nil for unknown key")
	}
}

func TestLookupAPIKeyConfig_NilReceiver(t *testing.T) {
	var cfg *Config
	if cfg.LookupAPIKeyConfig("k") != nil {
		t.Error("expected nil from nil receiver")
	}
}

func TestLookupModelGroup_FindsByName(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroup{
			{Name: "group-a", Models: []ModelGroupEntry{{Model: "m1", Priority: 1}}},
			{Name: "group-b"},
		},
	}
	result := cfg.LookupModelGroup("group-a")
	if result == nil || len(result.Models) != 1 {
		t.Fatalf("expected to find group-a with 1 model, got %v", result)
	}
}

func TestLookupModelGroup_ReturnsNilForUnknown(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroup{{Name: "existing"}},
	}
	if cfg.LookupModelGroup("missing") != nil {
		t.Fatal("expected nil for unknown group")
	}
}

func TestLookupModelGroup_NilReceiver(t *testing.T) {
	var cfg *Config
	if cfg.LookupModelGroup("g") != nil {
		t.Error("expected nil from nil receiver")
	}
}

func TestBuildAPIKeyConfigIndex_BuildsLookup(t *testing.T) {
	cfg := &Config{
		APIKeyConfigs: []APIKeyConfig{
			{Key: "k1", Label: "one"},
			{Key: "k2", Label: "two"},
		},
	}
	index := cfg.BuildAPIKeyConfigIndex()
	if len(index) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(index))
	}
	if index["k1"].Label != "one" {
		t.Errorf("wrong entry for k1: %v", index["k1"])
	}
}

func TestBuildAPIKeyConfigIndex_EmptyConfigs(t *testing.T) {
	cfg := &Config{}
	index := cfg.BuildAPIKeyConfigIndex()
	if len(index) != 0 {
		t.Errorf("expected empty index, got %v", index)
	}
}

func TestBuildModelGroupIndex_BuildsLookup(t *testing.T) {
	cfg := &Config{
		ModelGroups: []ModelGroup{
			{Name: "g1", Models: []ModelGroupEntry{{Model: "m1", Priority: 1}}},
		},
	}
	index := cfg.BuildModelGroupIndex()
	if len(index) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(index))
	}
	if _, ok := index["g1"]; !ok {
		t.Error("expected g1 in index")
	}
}

func TestBuildModelGroupIndex_EmptyGroups(t *testing.T) {
	cfg := &Config{}
	index := cfg.BuildModelGroupIndex()
	if len(index) != 0 {
		t.Errorf("expected empty index, got %v", index)
	}
}

func TestLoadConfig_APIKeyConfigsYAML(t *testing.T) {
	p := writeTestConfig(t, `
api-key-configs:
  - key: "team-a"
    label: "Team A"
    allowed-models:
      - "claude-sonnet-4"
      - "gemini-2.5-pro"
    routing:
      strategy: "fill-first"
  - key: "team-b"
    model-group: "claude-failover"

model-groups:
  - name: "claude-failover"
    models:
      - model: "claude-sonnet-4"
        priority: 3
      - model: "claude-3-haiku"
        priority: 1
`)
	cfg, err := LoadConfigOptional(p, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	if len(cfg.APIKeyConfigs) != 2 {
		t.Fatalf("expected 2 api-key-configs, got %d", len(cfg.APIKeyConfigs))
	}
	ka := cfg.APIKeyConfigs[0]
	if ka.Key != "team-a" || ka.Label != "Team A" {
		t.Errorf("unexpected first key config: %+v", ka)
	}
	if len(ka.AllowedModels) != 2 {
		t.Errorf("expected 2 allowed models, got %d", len(ka.AllowedModels))
	}
	if ka.Routing == nil || ka.Routing.Strategy != "fill-first" {
		t.Errorf("expected routing fill-first, got %v", ka.Routing)
	}
	kb := cfg.APIKeyConfigs[1]
	if kb.ModelGroup != "claude-failover" {
		t.Errorf("expected model-group 'claude-failover', got %q", kb.ModelGroup)
	}
	if len(cfg.ModelGroups) != 1 {
		t.Fatalf("expected 1 model group, got %d", len(cfg.ModelGroups))
	}
	g := cfg.ModelGroups[0]
	if g.Name != "claude-failover" || len(g.Models) != 2 {
		t.Errorf("unexpected model group: %+v", g)
	}
}

func TestLoadConfig_APIKeyConfigsMergedIntoFlatList(t *testing.T) {
	p := writeTestConfig(t, `
api-keys:
  - "legacy-key"
api-key-configs:
  - key: "new-key"
    label: "New"
`)
	cfg, err := LoadConfigOptional(p, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	found := false
	for _, k := range cfg.APIKeys {
		if k == "new-key" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'new-key' in flat api-keys list, got %v", cfg.APIKeys)
	}
	if len(cfg.APIKeys) != 2 {
		t.Errorf("expected 2 total keys (legacy + new), got %v", cfg.APIKeys)
	}
}

func TestLoadConfig_BackwardCompatible(t *testing.T) {
	/*
	 * Existing configs without api-key-configs/model-groups must parse cleanly
	 * with zero-value fields for the new additions.
	 */
	p := writeTestConfig(t, `
api-keys:
  - "legacy-key"
routing:
  strategy: "round-robin"
`)
	cfg, err := LoadConfigOptional(p, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	if len(cfg.APIKeys) != 1 || cfg.APIKeys[0] != "legacy-key" {
		t.Fatalf("expected legacy key preserved, got %v", cfg.APIKeys)
	}
	if len(cfg.APIKeyConfigs) != 0 {
		t.Fatalf("expected empty api-key-configs, got %v", cfg.APIKeyConfigs)
	}
	if len(cfg.ModelGroups) != 0 {
		t.Fatalf("expected empty model-groups, got %v", cfg.ModelGroups)
	}
}

func TestLoadConfig_RoutingNilWhenAbsent(t *testing.T) {
	p := writeTestConfig(t, `
api-key-configs:
  - key: "no-routing-key"
`)
	cfg, err := LoadConfigOptional(p, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	if cfg.APIKeyConfigs[0].Routing != nil {
		t.Errorf("expected nil Routing when not specified, got %v", cfg.APIKeyConfigs[0].Routing)
	}
}

func TestLoadConfig_PriorityField(t *testing.T) {
	p := writeTestConfig(t, `
api-key-configs:
  - key: "priority-key"
    priority: 5
`)
	cfg, err := LoadConfigOptional(p, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional error: %v", err)
	}
	kc := cfg.APIKeyConfigs[0]
	if kc.Priority == nil || *kc.Priority != 5 {
		t.Errorf("expected priority 5, got %v", kc.Priority)
	}
}

func TestModelGroupPriorityTiers(t *testing.T) {
	/*
	 * Documents the intended load-balancing + failover semantic:
	 * same-priority models are load-balanced; exhausted tiers fall through.
	 */
	g := ModelGroup{
		Name: "test",
		Models: []ModelGroupEntry{
			{Model: "a", Priority: 1},
			{Model: "b", Priority: 1},
			{Model: "c", Priority: 2},
			{Model: "d", Priority: 3},
		},
	}
	priorityCounts := make(map[int]int)
	for _, m := range g.Models {
		priorityCounts[m.Priority]++
	}
	if priorityCounts[1] != 2 {
		t.Errorf("expected 2 models at priority 1, got %d", priorityCounts[1])
	}
	if priorityCounts[2] != 1 || priorityCounts[3] != 1 {
		t.Errorf("unexpected priority distribution: %v", priorityCounts)
	}
}
