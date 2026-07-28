package config

import "strings"

// SanitizeAPIKeyConfigs normalizes per-key routing policy.
func (cfg *Config) SanitizeAPIKeyConfigs() {
	if cfg == nil || len(cfg.APIKeyConfigs) == 0 {
		return
	}
	out := make([]APIKeyConfig, 0, len(cfg.APIKeyConfigs))
	for _, keyConfig := range cfg.APIKeyConfigs {
		keyConfig.Key = strings.TrimSpace(keyConfig.Key)
		if keyConfig.Key == "" {
			continue
		}
		keyConfig.Label = strings.TrimSpace(keyConfig.Label)
		keyConfig.ModelGroup = strings.TrimSpace(keyConfig.ModelGroup)
		models := make([]string, 0, len(keyConfig.AllowedModels))
		for _, model := range keyConfig.AllowedModels {
			if model = strings.TrimSpace(model); model != "" {
				models = append(models, model)
			}
		}
		keyConfig.AllowedModels = models
		out = append(out, keyConfig)
	}
	cfg.APIKeyConfigs = out
}

// SanitizeModelGroups normalizes group names and drops empty members.
func (cfg *Config) SanitizeModelGroups() {
	if cfg == nil || len(cfg.ModelGroups) == 0 {
		return
	}
	out := make([]ModelGroup, 0, len(cfg.ModelGroups))
	for _, group := range cfg.ModelGroups {
		group.Name = strings.TrimSpace(group.Name)
		if group.Name == "" {
			continue
		}
		models := make([]ModelGroupEntry, 0, len(group.Models))
		for _, entry := range group.Models {
			if entry.Model = strings.TrimSpace(entry.Model); entry.Model != "" {
				models = append(models, entry)
			}
		}
		if len(models) == 0 {
			continue
		}
		group.Models = models
		out = append(out, group)
	}
	cfg.ModelGroups = out
}

// ReconcileAPIKeyConfigsIntoFlatList makes configured keys available to the access provider.
func (cfg *Config) ReconcileAPIKeyConfigsIntoFlatList(previous ...[]APIKeyConfig) {
	if cfg == nil {
		return
	}
	policyKeys := make(map[string]struct{}, len(cfg.APIKeyConfigs))
	for _, keyConfig := range cfg.APIKeyConfigs {
		if key := strings.TrimSpace(keyConfig.Key); key != "" {
			policyKeys[key] = struct{}{}
		}
	}
	previousKeys := make(map[string]struct{})
	if len(previous) > 0 {
		previousKeys = make(map[string]struct{}, len(previous[0]))
		for _, keyConfig := range previous[0] {
			if key := strings.TrimSpace(keyConfig.Key); key != "" {
				previousKeys[key] = struct{}{}
			}
		}
	}
	flatKeys := make([]string, 0, len(cfg.APIKeys)+len(policyKeys))
	for _, rawKey := range cfg.APIKeys {
		key := strings.TrimSpace(rawKey)
		if _, wasPolicy := previousKeys[key]; wasPolicy {
			continue
		}
		if _, isPolicy := policyKeys[key]; isPolicy {
			continue
		}
		flatKeys = append(flatKeys, rawKey)
	}
	added := make(map[string]struct{}, len(policyKeys))
	for _, keyConfig := range cfg.APIKeyConfigs {
		key := strings.TrimSpace(keyConfig.Key)
		if key == "" {
			continue
		}
		if _, ok := added[key]; ok {
			continue
		}
		flatKeys = append(flatKeys, key)
		added[key] = struct{}{}
	}
	cfg.APIKeys = flatKeys
}

func (cfg *Config) LookupAPIKeyConfig(key string) *APIKeyConfig {
	if cfg == nil {
		return nil
	}
	for i := range cfg.APIKeyConfigs {
		if cfg.APIKeyConfigs[i].Key == key {
			return &cfg.APIKeyConfigs[i]
		}
	}
	return nil
}

func (cfg *Config) LookupModelGroup(name string) *ModelGroup {
	if cfg == nil {
		return nil
	}
	for i := range cfg.ModelGroups {
		if cfg.ModelGroups[i].Name == name {
			return &cfg.ModelGroups[i]
		}
	}
	return nil
}

func (cfg *Config) BuildAPIKeyConfigIndex() map[string]*APIKeyConfig {
	index := make(map[string]*APIKeyConfig, len(cfg.APIKeyConfigs))
	if cfg == nil {
		return index
	}
	for i := range cfg.APIKeyConfigs {
		index[cfg.APIKeyConfigs[i].Key] = &cfg.APIKeyConfigs[i]
	}
	return index
}

func (cfg *Config) BuildModelGroupIndex() map[string]*ModelGroup {
	index := make(map[string]*ModelGroup, len(cfg.ModelGroups))
	if cfg == nil {
		return index
	}
	for i := range cfg.ModelGroups {
		index[cfg.ModelGroups[i].Name] = &cfg.ModelGroups[i]
	}
	return index
}
