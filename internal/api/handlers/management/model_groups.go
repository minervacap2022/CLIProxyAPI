package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// GetModelGroups returns the current model-groups list.
func (h *Handler) GetModelGroups(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"model-groups": h.cfg.ModelGroups})
}

// PutModelGroups replaces the entire model-groups list.
func (h *Handler) PutModelGroups(c *gin.Context) {
	var body struct {
		ModelGroups []config.ModelGroup `json:"model-groups"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	h.cfg.ModelGroups = append([]config.ModelGroup(nil), body.ModelGroups...)
	h.cfg.SanitizeModelGroups()
	h.keyConfigRefreshIfSet()
	h.persist(c)
}

// PatchModelGroup upserts a single ModelGroup entry matched by its name field.
// If an entry with the same name already exists it is replaced; otherwise it is appended.
func (h *Handler) PatchModelGroup(c *gin.Context) {
	var body struct {
		Value *config.ModelGroup `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	incoming := *body.Value
	incoming.Name = strings.TrimSpace(incoming.Name)
	if incoming.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name field is required"})
		return
	}
	for i := range h.cfg.ModelGroups {
		if h.cfg.ModelGroups[i].Name == incoming.Name {
			h.cfg.ModelGroups[i] = incoming
			h.cfg.SanitizeModelGroups()
			h.keyConfigRefreshIfSet()
			h.persist(c)
			return
		}
	}
	h.cfg.ModelGroups = append(h.cfg.ModelGroups, incoming)
	h.cfg.SanitizeModelGroups()
	h.keyConfigRefreshIfSet()
	h.persist(c)
}

// DeleteModelGroup removes the ModelGroup identified by the ?name= query parameter.
func (h *Handler) DeleteModelGroup(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name query parameter required"})
		return
	}
	out := h.cfg.ModelGroups[:0]
	for _, mg := range h.cfg.ModelGroups {
		if mg.Name != name {
			out = append(out, mg)
		}
	}
	h.cfg.ModelGroups = out
	h.cfg.SanitizeModelGroups()
	h.keyConfigRefreshIfSet()
	h.persist(c)
}
