package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// warmupPayload is the JSON envelope accepted by PUT /warmup.
// It mirrors config.WarmupConfig directly so the frontend can round-trip
// what it receives from GET /warmup.
type warmupPayload struct {
	Enabled     bool              `json:"enabled"`
	Interval    string            `json:"interval,omitempty"`
	StartAt     string            `json:"start-at,omitempty"`
	Timezone    string            `json:"timezone,omitempty"`
	OnStartup   bool              `json:"on-startup,omitempty"`
	Providers   []string          `json:"providers,omitempty"`
	Models      map[string]string `json:"models,omitempty"`
	Concurrency int               `json:"concurrency,omitempty"`
	Timeout     string            `json:"timeout,omitempty"`
}

// GetWarmup returns the current warmup configuration plus the list of
// providers that have a built-in warmup recipe, so the UI can render the
// provider picker without hardcoding the list.
func (h *Handler) GetWarmup(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusOK, gin.H{"warmup": config.WarmupConfig{}, "supported_providers": []string{}})
		return
	}
	supported := []string{}
	if h.warmupController != nil {
		supported = h.warmupController.SupportedProviders()
	}
	c.JSON(http.StatusOK, gin.H{
		"warmup":              h.cfg.Warmup,
		"supported_providers": supported,
	})
}

// PutWarmup replaces the warmup configuration. The scheduler is reloaded
// immediately so operators get instant feedback without a full restart.
//
// On validation failure, the old config is kept and a 400 is returned with
// a structured error so the frontend can highlight the offending field.
func (h *Handler) PutWarmup(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config_unavailable"})
		return
	}
	var payload warmupPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}

	// Snapshot for rollback if Reload rejects the new config.
	previous := h.cfg.Warmup
	h.cfg.Warmup = config.WarmupConfig{
		Enabled:     payload.Enabled,
		Interval:    payload.Interval,
		StartAt:     payload.StartAt,
		Timezone:    payload.Timezone,
		OnStartup:   payload.OnStartup,
		Providers:   payload.Providers,
		Models:      payload.Models,
		Concurrency: payload.Concurrency,
		Timeout:     payload.Timeout,
	}

	// If a scheduler is wired, validate by asking it to reload before we persist.
	// If no controller is wired (e.g. service embedded without warmup), we still
	// persist — the settings take effect on next startup.
	if h.warmupController != nil {
		if err := h.warmupController.Reload(); err != nil {
			h.cfg.Warmup = previous
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_warmup_config", "message": err.Error()})
			return
		}
	}

	if !h.persist(c) {
		// persist() already wrote an error response and rolled back to previous.
		// Reload the controller back to the old config to avoid drift.
		if h.warmupController != nil {
			_ = h.warmupController.Reload()
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"warmup": h.cfg.Warmup})
}

// TriggerWarmup fires a single warmup round synchronously. Returns 202 on
// start so the UI does not block on long-running upstream requests.
// The caller gets a JSON reason string they can display in the audit log.
func (h *Handler) TriggerWarmup(c *gin.Context) {
	if h == nil || h.warmupController == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "warmup_scheduler_unavailable"})
		return
	}
	reason := "manual"
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&payload); err == nil && payload.Reason != "" {
		reason = payload.Reason
	}
	go h.warmupController.TriggerNow(c.Copy().Request.Context(), reason)
	c.JSON(http.StatusAccepted, gin.H{"status": "triggered", "reason": reason})
}
