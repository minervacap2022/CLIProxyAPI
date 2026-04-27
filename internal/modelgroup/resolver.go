// Package modelgroup implements model group resolution and per-API-key model access checks.
// A model group is a named set of models with priority tiers; the proxy routes requests
// for the group name to the highest-priority available model, falling back to lower tiers
// when quota is exhausted.
package modelgroup

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// Tier represents a single priority level within a model group.
// All models within a tier are treated as equivalent for load balancing;
// the next tier is tried only after all models in the current tier are exhausted.
type Tier struct {
	// Priority is the numeric tier level. Higher values are preferred.
	Priority int
	// Models lists the model identifiers at this priority level, in config-defined order.
	Models []string
}

// AccessDeniedError is returned when a request uses a model not permitted by the API key config.
type AccessDeniedError struct {
	// Model is the model that was requested.
	Model string
	// Key is the API key that was denied (for logging; may be empty).
	Key string
}

func (e *AccessDeniedError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("model %q is not allowed for this API key", e.Model)
	}
	return fmt.Sprintf("model %q is not allowed for this API key", e.Model)
}

func (e *AccessDeniedError) StatusCode() int { return http.StatusForbidden }

// GroupByPriority groups model entries by their priority value and returns tiers sorted
// by priority descending (highest priority first). The order of models within each tier
// follows their original config order.
func GroupByPriority(entries []config.ModelGroupEntry) []Tier {
	if len(entries) == 0 {
		return nil
	}

	byPriority := make(map[int][]string)
	for _, e := range entries {
		byPriority[e.Priority] = append(byPriority[e.Priority], e.Model)
	}

	tiers := make([]Tier, 0, len(byPriority))
	for p, models := range byPriority {
		tiers = append(tiers, Tier{Priority: p, Models: models})
	}

	sort.Slice(tiers, func(i, j int) bool {
		return tiers[i].Priority > tiers[j].Priority
	})

	return tiers
}

// IsGroupModel reports whether the given name matches the group's name.
// Returns false when group is nil or name is empty.
func IsGroupModel(name string, group *config.ModelGroup) bool {
	if group == nil || name == "" {
		return false
	}
	return group.Name == name
}

// CheckModelAccess validates that the requested model is permitted by the API key config.
// Returns nil when access is allowed. Returns *AccessDeniedError when denied.
//
// Access rules:
//   - nil keyConfig → allow all (backward compatible, key has no extended config)
//   - AllowOtherModels true → allow all (explicit opt-in)
//   - empty AllowedModels + no ModelGroup → allow all
//   - only ModelGroup set → only the group name is allowed
//   - only ModelGroup set + AllowOtherModels → allow all (flag overrides restriction)
func CheckModelAccess(keyConfig *config.APIKeyConfig, model string) error {
	if keyConfig == nil {
		return nil
	}

	// Explicit opt-in: key may use any model.
	if keyConfig.AllowOtherModels {
		return nil
	}

	hasAllowedModels := len(keyConfig.AllowedModels) > 0
	hasModelGroup := keyConfig.ModelGroup != ""

	if !hasAllowedModels && !hasModelGroup {
		return nil
	}

	if hasAllowedModels {
		for _, allowed := range keyConfig.AllowedModels {
			if allowed == model {
				return nil
			}
		}
		if hasModelGroup && keyConfig.ModelGroup == model {
			return nil
		}
		return &AccessDeniedError{Model: model, Key: keyConfig.Key}
	}

	// Only ModelGroup is set: only the group name is permitted.
	if keyConfig.ModelGroup == model {
		return nil
	}
	return &AccessDeniedError{Model: model, Key: keyConfig.Key}
}
