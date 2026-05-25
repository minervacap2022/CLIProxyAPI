package usage

import (
	"context"
	"os"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

// TestPersistorRoundTrip exercises a real Redis (set via env). Skipped when
// CPA_USAGE_REDIS_ADDR is unset so the regular `go test` suite stays hermetic.
//
//	CPA_USAGE_REDIS_ADDR=127.0.0.1:6379 \
//	CPA_USAGE_REDIS_PASSWORD=... \
//	CPA_USAGE_REDIS_DB=15 \
//	go test -run TestPersistorRoundTrip ./internal/usage/...
func TestPersistorRoundTrip(t *testing.T) {
	addr := os.Getenv("CPA_USAGE_REDIS_ADDR")
	if addr == "" {
		t.Skip("CPA_USAGE_REDIS_ADDR not set; skipping live-redis test")
	}
	pwd := os.Getenv("CPA_USAGE_REDIS_PASSWORD")
	db := 15
	if v := os.Getenv("CPA_USAGE_REDIS_DB"); v != "" {
		if _, err := time.ParseDuration(v); err == nil {
			// allow numeric strings; we just need a non-default value here
		}
	}

	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "sk-test",
		Model:       "claude-sonnet-4-6",
		RequestedAt: time.Now(),
		Detail:      coreusage.Detail{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	})

	p, err := NewPersistor(PersistOptions{
		Addr:          addr,
		Password:      pwd,
		DB:            db,
		Key:           "cpa:usage:snapshot:test",
		FlushInterval: 50 * time.Millisecond,
	}, stats)
	if err != nil {
		t.Fatalf("NewPersistor: %v", err)
	}
	defer p.Stop()
	p.Start(context.Background())

	// allow at least one flush tick
	time.Sleep(150 * time.Millisecond)

	// Reload into a fresh stats object — should see the recorded request.
	freshStats := NewRequestStatistics()
	p2, err := NewPersistor(PersistOptions{
		Addr:     addr,
		Password: pwd,
		DB:       db,
		Key:      "cpa:usage:snapshot:test",
	}, freshStats)
	if err != nil {
		t.Fatalf("second NewPersistor: %v", err)
	}
	defer p2.Stop()
	if err := p2.LoadSnapshot(context.Background()); err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	snap := freshStats.Snapshot()
	if snap.TotalRequests != 1 {
		t.Fatalf("expected TotalRequests=1 after reload, got %d", snap.TotalRequests)
	}
	if snap.TotalTokens != 15 {
		t.Fatalf("expected TotalTokens=15 after reload, got %d", snap.TotalTokens)
	}
}
