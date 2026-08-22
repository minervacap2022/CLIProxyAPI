package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestSummaryRowsGroupsUsageAndFiltersByTimeRange(t *testing.T) {
	stats := NewRequestStatistics()
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	record := func(apiKey, model string, at time.Time, total int64, failed bool) {
		stats.Record(context.Background(), coreusage.Record{
			APIKey:      apiKey,
			Model:       model,
			RequestedAt: at,
			Failed:      failed,
			Detail: coreusage.Detail{
				InputTokens:     total / 2,
				OutputTokens:    total / 2,
				ReasoningTokens: 2,
				CachedTokens:    1,
				TotalTokens:     total,
			},
		})
	}

	record("key-a", "claude-sonnet-4-6", base, 10, false)
	record("key-a", "claude-sonnet-4-6", base.Add(time.Hour), 20, true)
	record("key-b", "claude-opus-4-6", base.Add(2*time.Hour), 30, false)

	rows := stats.SummaryRows(base.Add(-time.Minute), base.Add(90*time.Minute))
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	row := rows[0]
	if row.APIKey != "key-a" || row.Model != "claude-sonnet-4-6" {
		t.Fatalf("row identity = %+v", row)
	}
	if row.Requests != 2 || row.Failed != 1 || row.TotalTokens != 30 {
		t.Fatalf("row totals = %+v", row)
	}
	if row.InputTokens != 15 || row.OutputTokens != 15 || row.ReasoningTokens != 4 || row.CachedTokens != 2 {
		t.Fatalf("row token breakdown = %+v", row)
	}
}
