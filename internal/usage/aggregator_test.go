package usage

import (
	"context"
	"os"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

// newTestAggregator builds an aggregator with no Redis client. fold/Summary/
// prune never touch the client, so they are exercised hermetically.
func newTestAggregator(stats *RequestStatistics) *Aggregator {
	return &Aggregator{
		stats:        stats,
		buckets:      make(map[bucketKey]*aggBucket),
		cursor:       make(map[string]int),
		pendingDirty: make(map[bucketKey]struct{}),
		opts:         AggregatorOptions{Interval: defaultAggInterval},
	}
}

func TestAggregatorFoldGroupsByKeyModelHour(t *testing.T) {
	stats := NewRequestStatistics()
	base := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	rec := func(key, model string, at time.Time, in, out, total int64, failed bool) {
		stats.Record(context.Background(), coreusage.Record{
			APIKey:      key,
			Model:       model,
			RequestedAt: at,
			Failed:      failed,
			Detail:      coreusage.Detail{InputTokens: in, OutputTokens: out, TotalTokens: total},
		})
	}
	// Two requests same hour/key/model, one next hour, one different key.
	rec("sk-a", "opus", base, 10, 5, 15, false)
	rec("sk-a", "opus", base.Add(10*time.Minute), 20, 10, 30, true)
	rec("sk-a", "opus", base.Add(90*time.Minute), 1, 1, 2, false)
	rec("sk-b", "sonnet", base, 100, 50, 150, false)

	agg := newTestAggregator(stats)
	agg.fold()

	// Summary collapses hourly buckets back to api × model, so 2 rows.
	rows := agg.Summary(time.Time{}, time.Time{})
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (sk-a/opus + sk-b/sonnet), got %d: %+v", len(rows), rows)
	}

	var aOpus SummaryRow
	var bSonnet SummaryRow
	for _, r := range rows {
		switch {
		case r.APIKey == "sk-a" && r.Model == "opus":
			aOpus = r
		case r.APIKey == "sk-b" && r.Model == "sonnet":
			bSonnet = r
		}
	}
	if aOpus.Requests != 3 || aOpus.Failed != 1 || aOpus.TotalTokens != 47 {
		t.Fatalf("sk-a/opus wrong: %+v", aOpus)
	}
	if bSonnet.Requests != 1 || bSonnet.TotalTokens != 150 {
		t.Fatalf("sk-b/sonnet wrong: %+v", bSonnet)
	}

	// sk-a/opus must occupy two distinct hour buckets internally.
	hours := 0
	for k := range agg.buckets {
		if k.api == "sk-a" && k.model == "opus" {
			hours++
		}
	}
	if hours != 2 {
		t.Fatalf("expected 2 hour buckets for sk-a/opus, got %d", hours)
	}
}

func TestAggregatorFoldIsIncremental(t *testing.T) {
	stats := NewRequestStatistics()
	now := time.Now()
	stats.Record(context.Background(), coreusage.Record{APIKey: "sk", Model: "m", RequestedAt: now, Detail: coreusage.Detail{TotalTokens: 5}})

	agg := newTestAggregator(stats)
	agg.fold()
	if got := agg.Summary(time.Time{}, time.Time{}); len(got) != 1 || got[0].Requests != 1 {
		t.Fatalf("first fold wrong: %+v", got)
	}

	// Second fold with no new details must not double-count.
	agg.fold()
	if got := agg.Summary(time.Time{}, time.Time{}); got[0].Requests != 1 {
		t.Fatalf("re-fold double counted: %+v", got)
	}

	// New record gets folded incrementally.
	stats.Record(context.Background(), coreusage.Record{APIKey: "sk", Model: "m", RequestedAt: now, Detail: coreusage.Detail{TotalTokens: 7}})
	agg.fold()
	got := agg.Summary(time.Time{}, time.Time{})
	if got[0].Requests != 2 || got[0].TotalTokens != 12 {
		t.Fatalf("incremental fold wrong: %+v", got)
	}
}

func TestAggregatorSummaryTimeWindow(t *testing.T) {
	stats := NewRequestStatistics()
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()
	stats.Record(context.Background(), coreusage.Record{APIKey: "sk", Model: "m", RequestedAt: old, Detail: coreusage.Detail{TotalTokens: 5}})
	stats.Record(context.Background(), coreusage.Record{APIKey: "sk", Model: "m", RequestedAt: recent, Detail: coreusage.Detail{TotalTokens: 9}})

	agg := newTestAggregator(stats)
	agg.fold()

	rows := agg.Summary(time.Now().Add(-1*time.Hour), time.Time{})
	var total int64
	for _, r := range rows {
		total += r.TotalTokens
	}
	if total != 9 {
		t.Fatalf("expected only recent bucket (9) in window, got %d", total)
	}
}

func TestAggregatorBucketFieldRoundTrip(t *testing.T) {
	k := bucketKey{api: "sk-x", model: "claude-opus-4-8", hour: 482000}
	got, ok := parseBucketField(k.field())
	if !ok || got != k {
		t.Fatalf("round trip failed: ok=%v got=%+v want=%+v", ok, got, k)
	}
}

// TestAggregatorRedisRoundTrip exercises a real Redis (set via env). Skipped
// when CPA_USAGE_REDIS_ADDR is unset so the regular suite stays hermetic.
func TestAggregatorRedisRoundTrip(t *testing.T) {
	addr := os.Getenv("CPA_USAGE_REDIS_ADDR")
	if addr == "" {
		t.Skip("CPA_USAGE_REDIS_ADDR not set; skipping live-redis test")
	}
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{APIKey: "sk", Model: "m", RequestedAt: time.Now(), Detail: coreusage.Detail{TotalTokens: 11}})

	agg, err := NewAggregator(AggregatorOptions{Addr: addr, Password: os.Getenv("CPA_USAGE_REDIS_PASSWORD"), DB: 15, Key: "cpa:test:agg"}, stats)
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	defer func() {
		agg.client.Del(context.Background(), agg.bucketKey, agg.cursorKey)
		agg.Stop()
	}()
	agg.tick(context.Background())

	// Fresh aggregator loads persisted buckets.
	agg2, err := NewAggregator(AggregatorOptions{Addr: addr, Password: os.Getenv("CPA_USAGE_REDIS_PASSWORD"), DB: 15, Key: "cpa:test:agg"}, NewRequestStatistics())
	if err != nil {
		t.Fatalf("NewAggregator 2: %v", err)
	}
	defer agg2.Stop()
	if err := agg2.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	rows := agg2.Summary(time.Time{}, time.Time{})
	if len(rows) != 1 || rows[0].TotalTokens != 11 {
		t.Fatalf("redis round trip wrong: %+v", rows)
	}
}

func TestAggregatorFoldSurvivesDetailEviction(t *testing.T) {
	prev := int(maxDetailsPerModel.Load())
	SetMaxDetailsPerModel(5)
	defer SetMaxDetailsPerModel(prev)

	stats := NewRequestStatistics()
	now := time.Now()
	rec := func(tok int64) {
		stats.Record(context.Background(), coreusage.Record{
			APIKey:      "k",
			Model:       "m",
			RequestedAt: now,
			Detail:      coreusage.Detail{TotalTokens: tok},
		})
	}

	agg := newTestAggregator(stats)

	// First 3 requests, fold them.
	rec(1)
	rec(1)
	rec(1)
	agg.fold()
	if got := agg.Summary(time.Time{}, time.Time{}); got[0].Requests != 3 || got[0].TotalTokens != 3 {
		t.Fatalf("after first fold: %+v", got)
	}

	// 4 more requests push total to 7, evicting the 2 oldest (cap=5). The
	// evicted ones were already folded, so no data is lost.
	rec(1)
	rec(1)
	rec(1)
	rec(1)
	agg.fold()
	got := agg.Summary(time.Time{}, time.Time{})
	if got[0].Requests != 7 || got[0].TotalTokens != 7 {
		t.Fatalf("after eviction fold: requests=%d tokens=%d, want 7/7", got[0].Requests, got[0].TotalTokens)
	}

	// No new requests: fold must be a no-op (no double counting).
	agg.fold()
	if got := agg.Summary(time.Time{}, time.Time{}); got[0].Requests != 7 {
		t.Fatalf("re-fold double counted: %+v", got)
	}
}

func TestAggregatorRetainsDirtyBatchAfterFlushFailure(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{APIKey: "retry", Model: "m", RequestedAt: time.Now(), Detail: coreusage.Detail{TotalTokens: 13}})
	agg := newTestAggregator(stats)
	agg.pendingDirty[bucketKey{api: "existing", model: "m", hour: 1}] = struct{}{}
	agg.tick(context.Background())
	if len(agg.pendingDirty) != 2 {
		t.Fatalf("pending dirty buckets = %d, want 2 after failed flush", len(agg.pendingDirty))
	}
}
