package api

import (
	"github.com/gin-gonic/gin"
	managementHandlers "github.com/router-for-me/CLIProxyAPI/v7/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func (s *Server) ManagementHandler() *managementHandlers.Handler {
	if s == nil {
		return nil
	}
	return s.mgmt
}

func (s *Server) rebuildKeyConfigIndexes(cfg *config.Config) {
	if s == nil || cfg == nil {
		return
	}
	s.apiKeyConfigIndex.Store(cfg.BuildAPIKeyConfigIndex())
	s.modelGroupIndex.Store(cfg.BuildModelGroupIndex())
}

func (s *Server) keyConfigMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rawKey, exists := c.Get("userApiKey")
		if !exists {
			c.Next()
			return
		}
		key, _ := rawKey.(string)
		if key == "" {
			c.Next()
			return
		}
		if rawIndex := s.apiKeyConfigIndex.Load(); rawIndex != nil {
			if keyConfig := rawIndex.(map[string]*config.APIKeyConfig)[key]; keyConfig != nil {
				c.Set("apiKeyConfig", keyConfig)
				if keyConfig.ModelGroup != "" {
					if rawGroups := s.modelGroupIndex.Load(); rawGroups != nil {
						if group := rawGroups.(map[string]*config.ModelGroup)[keyConfig.ModelGroup]; group != nil {
							c.Set("modelGroup", group)
						}
					}
				}
			}
		}
		c.Next()
	}
}
