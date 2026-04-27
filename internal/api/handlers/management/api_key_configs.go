package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// GetAPIKeyConfigs returns the current api-key-configs list.
func (h *Handler) GetAPIKeyConfigs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"api-key-configs": h.cfg.APIKeyConfigs})
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
	h.cfg.APIKeyConfigs = append([]config.APIKeyConfig(nil), body.APIKeyConfigs...)
	h.cfg.SanitizeAPIKeyConfigs()
	h.cfg.MergeAPIKeyConfigsIntoFlatList()
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
	for i := range h.cfg.APIKeyConfigs {
		if h.cfg.APIKeyConfigs[i].Key == incoming.Key {
			h.cfg.APIKeyConfigs[i] = incoming
			h.cfg.SanitizeAPIKeyConfigs()
			h.cfg.MergeAPIKeyConfigsIntoFlatList()
			h.keyConfigRefreshIfSet()
			h.persist(c)
			return
		}
	}
	h.cfg.APIKeyConfigs = append(h.cfg.APIKeyConfigs, incoming)
	h.cfg.SanitizeAPIKeyConfigs()
	h.cfg.MergeAPIKeyConfigsIntoFlatList()
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
	out := h.cfg.APIKeyConfigs[:0]
	for _, kc := range h.cfg.APIKeyConfigs {
		if kc.Key != key {
			out = append(out, kc)
		}
	}
	h.cfg.APIKeyConfigs = out
	h.cfg.SanitizeAPIKeyConfigs()
	h.cfg.MergeAPIKeyConfigsIntoFlatList()
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
