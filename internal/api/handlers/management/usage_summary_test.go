package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestGetUsageSummaryReturnsFilteredTokenRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stats := usage.NewRequestStatistics()
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "team-key",
		Model:       "claude-sonnet-4-6",
		RequestedAt: base,
		Detail:      coreusage.Detail{TotalTokens: 42},
	})
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "team-key",
		Model:       "claude-sonnet-4-6",
		RequestedAt: base.Add(24 * time.Hour),
		Detail:      coreusage.Detail{TotalTokens: 99},
	})

	handler := &Handler{usageStats: stats}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/v0/management/usage/summary?start=2026-08-20T00:00:00Z&end=2026-08-20T23:59:59Z", nil)
	ctx.Request = request
	handler.GetUsageSummary(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Rows []usage.SummaryRow `json:"rows"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Rows) != 1 || response.Rows[0].TotalTokens != 42 {
		t.Fatalf("rows = %+v, want filtered total 42", response.Rows)
	}
}
