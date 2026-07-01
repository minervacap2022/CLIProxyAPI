// Package management — legacy in-memory usage statistics endpoints.
//
// Klik fork keeps the original upstream v6 endpoints alive so the bundled
// management panel and downstream tooling can still read usage stats:
//
//	GET  /v0/management/usage          → live snapshot
//	GET  /v0/management/usage/export   → snapshot wrapped in versioned envelope
//	POST /v0/management/usage/import   → merge a snapshot back in
//
// These complement (do not replace) the new /usage-queue endpoint that
// upstream introduced as the Redis-queue consumer entry point.
package management

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

type usageExportPayload struct {
	Version    int                      `json:"version"`
	ExportedAt time.Time                `json:"exported_at"`
	Usage      usage.StatisticsSnapshot `json:"usage"`
}

type usageImportPayload struct {
	Version int                      `json:"version"`
	Usage   usage.StatisticsSnapshot `json:"usage"`
}

// GetUsageSummary returns api_key × model aggregated usage rows for the optional
// [start, end] window (RFC3339 or unix-millis query params). This powers the
// management panel Team view without shipping every per-request detail to the
// browser.
func (h *Handler) GetUsageSummary(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusOK, gin.H{"rows": []usage.SummaryRow{}})
		return
	}
	start := parseUsageTime(c.Query("start"))
	end := parseUsageTime(c.Query("end"))
	if agg := usage.DefaultAggregator(); agg != nil {
		c.JSON(http.StatusOK, gin.H{"rows": agg.Summary(start, end), "source": "aggregator"})
		return
	}
	rows := h.usageStats.SummaryRows(start, end)
	c.JSON(http.StatusOK, gin.H{"rows": rows, "source": "memory"})
}

// GetUsageTimeseries returns total usage points grouped by hour or day for the
// optional [start, end] window. This powers the lightweight usage charts
// without returning the full per-request snapshot.
func (h *Handler) GetUsageTimeseries(c *gin.Context) {
	start := parseUsageTime(c.Query("start"))
	end := parseUsageTime(c.Query("end"))
	bucket := usage.ResolveUsageBucket(c.Query("bucket"), start, end)

	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusOK, gin.H{
			"bucket": bucket,
			"points": []usage.TimeseriesPoint{},
			"rows":   []usage.TimeseriesRow{},
		})
		return
	}
	if agg := usage.DefaultAggregator(); agg != nil {
		c.JSON(http.StatusOK, gin.H{
			"bucket": bucket,
			"points": agg.Timeseries(start, end, bucket),
			"rows":   agg.TimeseriesRows(start, end, bucket),
			"source": "aggregator",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"bucket": bucket,
		"points": h.usageStats.Timeseries(start, end, bucket),
		"rows":   h.usageStats.TimeseriesRows(start, end, bucket),
		"source": "memory",
	})
}

// parseUsageTime parses a query value as RFC3339 or unix milliseconds. An empty
// or unparseable value yields the zero time (treated as unbounded).
func parseUsageTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	if ms, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.UnixMilli(ms)
	}
	return time.Time{}
}

// GetUsageStatistics returns the in-memory request statistics snapshot.
func (h *Handler) GetUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, gin.H{
		"usage":           snapshot,
		"failed_requests": snapshot.FailureCount,
	})
}

// ExportUsageStatistics returns a complete usage snapshot for backup/migration.
func (h *Handler) ExportUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, usageExportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC(),
		Usage:      snapshot,
	})
}

// ImportUsageStatistics merges a previously exported usage snapshot into memory.
func (h *Handler) ImportUsageStatistics(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage statistics unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var payload usageImportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if payload.Version != 0 && payload.Version != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported version"})
		return
	}

	result := h.usageStats.MergeSnapshot(payload.Usage)
	snapshot := h.usageStats.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"added":           result.Added,
		"skipped":         result.Skipped,
		"total_requests":  snapshot.TotalRequests,
		"failed_requests": snapshot.FailureCount,
	})
}
