package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	usageinternal "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestGetUsageTimeseriesReturnsHourlyPoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := usageinternal.NewRequestStatistics()
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "sk-a",
		Model:       "gpt-5",
		RequestedAt: base.Add(10 * time.Minute),
		Detail:      coreusage.Detail{TotalTokens: 12},
	})
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "sk-a",
		Model:       "gpt-5",
		RequestedAt: base.Add(70 * time.Minute),
		Failed:      true,
		Detail:      coreusage.Detail{TotalTokens: 8},
	})

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v0/management/usage/timeseries?start=2026-01-02T09:00:00Z&end=2026-01-02T12:00:00Z&bucket=hour",
		nil,
	)

	h := &Handler{usageStats: stats}
	h.GetUsageTimeseries(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Bucket string                          `json:"bucket"`
		Points []usageinternal.TimeseriesPoint `json:"points"`
		Source string                          `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Bucket != string(usageinternal.UsageBucketHour) {
		t.Fatalf("bucket = %q", payload.Bucket)
	}
	if payload.Source != "memory" {
		t.Fatalf("source = %q", payload.Source)
	}
	if len(payload.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(payload.Points))
	}
	if payload.Points[0].TotalTokens != 12 || payload.Points[1].Failed != 1 {
		t.Fatalf("points = %+v", payload.Points)
	}
}
