package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestRequestStatisticsRecordIncludesLatency(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Latency:     1500 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("latency_ms = %d, want 1500", details[0].LatencyMs)
	}
}

func TestRequestStatisticsMergeSnapshotDedupIgnoresLatency(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	first := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 0,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}
	second := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(first)
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("first merge = %+v, want added=1 skipped=0", result)
	}

	result = stats.MergeSnapshot(second)
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("second merge = %+v, want added=0 skipped=1", result)
	}

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
}

func TestRequestStatisticsDetailsRingBufferCap(t *testing.T) {
	prev := int(maxDetailsPerModel.Load())
	SetMaxDetailsPerModel(5)
	defer SetMaxDetailsPerModel(prev)

	stats := NewRequestStatistics()
	base := time.Now()
	for i := 0; i < 12; i++ {
		stats.Record(context.Background(), coreusage.Record{
			APIKey:      "k",
			Model:       "m",
			RequestedAt: base.Add(time.Duration(i) * time.Second),
			Detail:      coreusage.Detail{TotalTokens: int64(i + 1)},
		})
	}

	ms := stats.apis["k"].Models["m"]
	if len(ms.Details) != 5 {
		t.Fatalf("retained details = %d, want 5", len(ms.Details))
	}
	if ms.DetailsBase != 7 {
		t.Fatalf("DetailsBase = %d, want 7", ms.DetailsBase)
	}
	// Aggregated counters must reflect all 12 requests, not just retained ones.
	if ms.TotalRequests != 12 {
		t.Fatalf("TotalRequests = %d, want 12", ms.TotalRequests)
	}
	// Retained window must be the newest entries (tokens 8..12).
	if ms.Details[0].Tokens.TotalTokens != 8 || ms.Details[4].Tokens.TotalTokens != 12 {
		t.Fatalf("retained window wrong: first=%d last=%d", ms.Details[0].Tokens.TotalTokens, ms.Details[4].Tokens.TotalTokens)
	}
}

func TestRequestStatisticsRestorePreservesTotalsBeyondDetailCap(t *testing.T) {
	prev := int(maxDetailsPerModel.Load())
	SetMaxDetailsPerModel(5)
	defer SetMaxDetailsPerModel(prev)

	stats := NewRequestStatistics()
	base := time.Now()
	for i := 0; i < 12; i++ {
		stats.Record(context.Background(), coreusage.Record{APIKey: "k", Model: "m", RequestedAt: base.Add(time.Duration(i) * time.Second), Detail: coreusage.Detail{TotalTokens: 1}})
	}
	snapshot := stats.Snapshot()
	restored := NewRequestStatistics()
	restored.MergeSnapshot(snapshot)
	got := restored.Snapshot()
	if got.TotalRequests != 12 || got.TotalTokens != 12 {
		t.Fatalf("restored totals = %d/%d, want 12/12", got.TotalRequests, got.TotalTokens)
	}
	model := restored.apis["k"].Models["m"]
	if model.TotalRequests != 12 || model.DetailsBase != 7 || len(model.Details) != 5 {
		t.Fatalf("restored model = %+v", model)
	}
	agg := newTestAggregator(restored)
	agg.cursor["k"+aggFieldSep+"m"] = int(model.DetailsBase + int64(len(model.Details)))
	if dirty := agg.fold(); len(dirty) != 0 {
		t.Fatalf("restored cursor refolded %d buckets", len(dirty))
	}
}
