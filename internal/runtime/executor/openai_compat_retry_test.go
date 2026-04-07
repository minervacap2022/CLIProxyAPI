package executor

import (
	"fmt"
	"testing"
	"time"
)

func TestParseDoubaoRetryAfter(t *testing.T) {
	future := time.Now().Add(10 * 24 * time.Hour)
	msg := fmt.Sprintf(
		`{"error":{"code":"AccountQuotaExceeded","message":"You have exceeded the monthly usage quota. It will reset at %s. We recommend upgrading.","type":"TooManyRequests"}}`,
		future.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05 +0800 CST"),
	)

	d := parseDoubaoRetryAfter([]byte(msg))
	if d == nil {
		t.Fatal("expected non-nil duration for valid reset timestamp")
	}
	if *d < 9*24*time.Hour || *d > 11*24*time.Hour {
		t.Errorf("expected ~10d duration, got %v", *d)
	}

	// Exact body from real 429 response
	realBody := []byte(`{"error":{"code":"AccountQuotaExceeded","message":"You have exceeded the monthly usage quota. It will reset at 2026-04-23 23:59:59 +0800 CST. We recommend upgrading your plan for more quota, or waiting for the reset.","param":"","type":"TooManyRequests"}}`)
	d2 := parseDoubaoRetryAfter(realBody)
	if d2 == nil {
		t.Fatal("expected non-nil duration for real 429 body")
	}

	// No reset timestamp → nil
	if parseDoubaoRetryAfter([]byte(`{"error":{"message":"rate limit"}}`)) != nil {
		t.Error("expected nil for missing reset timestamp")
	}

	// Empty body → nil
	if parseDoubaoRetryAfter(nil) != nil {
		t.Error("expected nil for nil body")
	}
}
