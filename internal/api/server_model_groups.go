package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	claudemodels "github.com/router-for-me/CLIProxyAPI/v7/internal/client/claude/models"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/claude"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/openai"
)

func (s *Server) modelGroupEntries() []map[string]any {
	if s == nil || s.cfg == nil || len(s.cfg.ModelGroups) == 0 {
		return nil
	}
	entries := make([]map[string]any, 0, len(s.cfg.ModelGroups))
	for _, group := range s.cfg.ModelGroups {
		entries = append(entries, map[string]any{
			"id":           group.Name,
			"object":       "model",
			"created":      0,
			"owned_by":     "model-group",
			"type":         "model-group",
			"display_name": group.Name,
		})
	}
	return entries
}

func (s *Server) serveOpenAIModelsWithGroups(c *gin.Context, handler *openai.OpenAIAPIHandler) {
	models := handler.Models()
	filtered := make([]map[string]any, 0, len(models)+len(s.modelGroupEntries()))
	for _, model := range models {
		entry := map[string]any{"id": model["id"], "object": model["object"]}
		if created, ok := model["created"]; ok {
			entry["created"] = created
		}
		if owner, ok := model["owned_by"]; ok {
			entry["owned_by"] = owner
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, s.modelGroupEntries()...)
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": filtered})
}

func (s *Server) serveClaudeModelsWithGroups(c *gin.Context, handler *claude.ClaudeCodeAPIHandler) {
	models := append(handler.Models(), s.modelGroupEntries()...)
	disableCloaking := handler.Cfg != nil && handler.Cfg.ClaudeCode.DisableCloakingModelList
	c.JSON(http.StatusOK, claudemodels.BuildResponse(models, disableCloaking))
}
