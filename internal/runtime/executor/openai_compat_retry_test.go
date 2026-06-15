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

	// Exact body shape from a real 429 response, with a future reset timestamp
	// (relative so the test does not expire as wall-clock time advances).
	realReset := future.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05 +0800 CST")
	realBody := []byte(fmt.Sprintf(`{"error":{"code":"AccountQuotaExceeded","message":"You have exceeded the monthly usage quota. It will reset at %s. We recommend upgrading your plan for more quota, or waiting for the reset.","param":"","type":"TooManyRequests"}}`, realReset))
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

func TestParseDoubaoRetryAfter_BurstTooFast(t *testing.T) {
	body := []byte(`{"error":{"code":"RequestBurstTooFast","message":"System protection triggered by request burst. Please slow down traffic growth and increase requests gradually before retrying.","param":"","type":"TooManyRequests"}}`)
	d := parseDoubaoRetryAfter(body)
	if d == nil {
		t.Fatal("expected non-nil duration for RequestBurstTooFast")
	}
	if *d != doubaoBurstCooldown {
		t.Errorf("expected %v, got %v", doubaoBurstCooldown, *d)
	}

	// Unrelated 429 (e.g., generic rate limit) → still nil
	if parseDoubaoRetryAfter([]byte(`{"error":{"code":"RateLimitExceeded","message":"too many requests"}}`)) != nil {
		t.Error("expected nil for unrecognized 429 code")
	}
}
