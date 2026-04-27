package synthesizer

import (
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type refreshControlRuntime struct {
	disabled map[string]struct{}
}

func newRefreshControlRuntime(providers []string) *refreshControlRuntime {
	disabled := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		key := strings.ToLower(strings.TrimSpace(provider))
		if key == "" {
			continue
		}
		disabled[key] = struct{}{}
	}
	if len(disabled) == 0 {
		return nil
	}
	return &refreshControlRuntime{disabled: disabled}
}

func (r *refreshControlRuntime) refreshDisabled(provider string) bool {
	if r == nil {
		return false
	}
	_, ok := r.disabled[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}

func (r *refreshControlRuntime) ShouldRefresh(now time.Time, auth *coreauth.Auth) bool {
	if auth == nil {
		return false
	}
	return !r.refreshDisabled(auth.Provider)
}

func (r *refreshControlRuntime) RefreshLead() *time.Duration {
	return nil
}
