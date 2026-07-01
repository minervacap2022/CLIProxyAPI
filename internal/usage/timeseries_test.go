package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestRequestStatisticsTimeseriesGroupsByHour(t *testing.T) {
	stats := NewRequestStatistics()
	base := time.Date(2026, 1, 2, 10, 15, 0, 0, time.UTC)

	record := func(at time.Time, total int64, failed bool) {
		stats.Record(context.Background(), coreusage.Record{
			APIKey:      "sk-a",
			Model:       "gpt-5",
			RequestedAt: at,
			Failed:      failed,
			Detail: coreusage.Detail{
				InputTokens:  total / 2,
				OutputTokens: total / 2,
				TotalTokens:  total,
			},
		})
	}

	record(base, 20, false)
	record(base.Add(20*time.Minute), 10, true)
	record(base.Add(90*time.Minute), 8, false)

	points := stats.Timeseries(base.Add(-time.Minute), base.Add(2*time.Hour), UsageBucketHour)
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}

	first := points[0]
	if !first.Start.Equal(time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("first start = %s", first.Start)
	}
	if first.Requests != 2 || first.Failed != 1 || first.TotalTokens != 30 {
		t.Fatalf("first point = %+v", first)
	}

	second := points[1]
	if !second.Start.Equal(time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("second start = %s", second.Start)
	}
	if second.Requests != 1 || second.Failed != 0 || second.TotalTokens != 8 {
		t.Fatalf("second point = %+v", second)
	}
}

func TestAggregatorTimeseriesAggregatesByDay(t *testing.T) {
	stats := NewRequestStatistics()
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	record := func(at time.Time, total int64) {
		stats.Record(context.Background(), coreusage.Record{
			APIKey:      "sk-a",
			Model:       "gpt-5",
			RequestedAt: at,
			Detail: coreusage.Detail{
				TotalTokens: total,
			},
		})
	}

	record(base, 5)
	record(base.Add(2*time.Hour), 7)
	record(base.Add(26*time.Hour), 11)

	agg := newTestAggregator(stats)
	agg.fold()

	points := agg.Timeseries(base.Add(-time.Hour), base.Add(48*time.Hour), UsageBucketDay)
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	if points[0].Requests != 2 || points[0].TotalTokens != 12 {
		t.Fatalf("day1 point = %+v", points[0])
	}
	if points[1].Requests != 1 || points[1].TotalTokens != 11 {
		t.Fatalf("day2 point = %+v", points[1])
	}
}
