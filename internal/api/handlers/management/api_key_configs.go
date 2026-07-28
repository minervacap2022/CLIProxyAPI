package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// GetAPIKeyConfigs returns api-key-configs plus read-visible rows for flat api-keys
// that do not yet have per-key config. The flat api-keys list is the auth
// source of truth, so the management UI must show those keys instead of hiding
// active keys that can accumulate usage. Synthetic rows are not persisted unless
// the caller later PATCHes/PUTs them.
func (h *Handler) GetAPIKeyConfigs(c *gin.Context) {
	configs := append([]config.APIKeyConfig(nil), h.cfg.APIKeyConfigs...)
	seen := make(map[string]struct{}, len(configs))
	for _, kc := range configs {
		key := strings.TrimSpace(kc.Key)
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, raw := range h.cfg.APIKeys {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		label := "Unconfigured key"
		if strings.HasPrefix(key, "cli-proxy-api-default-auto-created-") {
			label = "Default auto-created"
		}
		configs = append(configs, config.APIKeyConfig{Key: key, Label: label, AllowOtherModels: true})
		seen[key] = struct{}{}
	}
	c.JSON(http.StatusOK, gin.H{"api-key-configs": configs})
}

// PutAPIKeyConfigs replaces the entire api-key-configs list and re-merges the flat api-keys list.
func (h *Handler) PutAPIKeyConfigs(c *gin.Context) {
	var body struct {
		APIKeyConfigs []config.APIKeyConfig `json:"api-key-configs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	previous := h.cfg.APIKeyConfigs
	h.cfg.APIKeyConfigs = append([]config.APIKeyConfig(nil), body.APIKeyConfigs...)
	h.cfg.SanitizeAPIKeyConfigs()
	h.cfg.ReconcileAPIKeyConfigsIntoFlatList(previous)
	h.keyConfigRefreshIfSet()
	h.persist(c)
}

// PatchAPIKeyConfig upserts a single APIKeyConfig entry matched by its key field.
// If an entry with the same key already exists it is replaced; otherwise it is appended.
func (h *Handler) PatchAPIKeyConfig(c *gin.Context) {
	var body struct {
		Value *config.APIKeyConfig `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	incoming := *body.Value
	incoming.Key = strings.TrimSpace(incoming.Key)
	if incoming.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key field is required"})
		return
	}
	previous := append([]config.APIKeyConfig(nil), h.cfg.APIKeyConfigs...)
	for i := range h.cfg.APIKeyConfigs {
		if h.cfg.APIKeyConfigs[i].Key == incoming.Key {
			h.cfg.APIKeyConfigs[i] = incoming
			h.cfg.SanitizeAPIKeyConfigs()
			h.cfg.ReconcileAPIKeyConfigsIntoFlatList(previous)
			h.keyConfigRefreshIfSet()
			h.persist(c)
			return
		}
	}
	h.cfg.APIKeyConfigs = append(h.cfg.APIKeyConfigs, incoming)
	h.cfg.SanitizeAPIKeyConfigs()
	h.cfg.ReconcileAPIKeyConfigsIntoFlatList(previous)
	h.keyConfigRefreshIfSet()
	h.persist(c)
}

// DeleteAPIKeyConfig removes the APIKeyConfig entry identified by the ?key= query parameter.
func (h *Handler) DeleteAPIKeyConfig(c *gin.Context) {
	key := strings.TrimSpace(c.Query("key"))
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key query parameter required"})
		return
	}
	previous := append([]config.APIKeyConfig(nil), h.cfg.APIKeyConfigs...)
	out := h.cfg.APIKeyConfigs[:0]
	for _, kc := range h.cfg.APIKeyConfigs {
		if kc.Key != key {
			out = append(out, kc)
		}
	}
	h.cfg.APIKeyConfigs = out
	h.cfg.SanitizeAPIKeyConfigs()
	h.cfg.ReconcileAPIKeyConfigsIntoFlatList(previous)
	h.keyConfigRefreshIfSet()
	h.persist(c)
}

/*
keyConfigRefreshIfSet calls the optional refresh callback registered by the server.
This triggers an immediate rebuild of the in-memory key-config and model-group
lookup indexes so changes take effect on the next request without waiting for
the file-watcher reload cycle.
*/
func (h *Handler) keyConfigRefreshIfSet() {
	if h.keyConfigRefreshFunc != nil {
		h.keyConfigRefreshFunc()
	}
}
