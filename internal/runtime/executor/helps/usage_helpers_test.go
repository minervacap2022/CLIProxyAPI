package helps

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail := ParseOpenAIUsage(data)
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

// TestParseOpenAIStreamUsage_DoubaoNullOnIntermediate reproduces the issue
// where Doubao emits "usage":null on intermediate streaming chunks. The parser
// must return ok=false for these, otherwise the UsageReporter's sync.Once
// fires with zero tokens and the real usage chunk is silently dropped.
func TestParseOpenAIStreamUsage_DoubaoNullOnIntermediate(t *testing.T) {
	intermediate := []byte(`data: {"choices":[{"delta":{"content":"","reasoning_content":"\n","role":"assistant"},"index":0}],"created":1,"id":"x","model":"doubao-seed-2-0-pro","object":"chat.completion.chunk","usage":null}`)
	if _, ok := ParseOpenAIStreamUsage(intermediate); ok {
		t.Fatalf("expected ok=false for usage:null, parser would record 0 tokens")
	}

	final := []byte(`data: {"choices":[],"created":1,"id":"x","model":"doubao-seed-2-0-pro","object":"chat.completion.chunk","usage":{"completion_tokens":119,"prompt_tokens":51,"total_tokens":170,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":110}}}`)
	detail, ok := ParseOpenAIStreamUsage(final)
	if !ok {
		t.Fatal("expected ok=true for final chunk with real usage")
	}
	if detail.InputTokens != 51 || detail.OutputTokens != 119 || detail.TotalTokens != 170 || detail.ReasoningTokens != 110 {
		t.Errorf("got input=%d output=%d total=%d reasoning=%d; want 51/119/170/110",
			detail.InputTokens, detail.OutputTokens, detail.TotalTokens, detail.ReasoningTokens)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail := ParseOpenAIUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
}

func TestUsageReporterBuildRecordIncludesLatency(t *testing.T) {
	reporter := &UsageReporter{
		provider:    "openai",
		model:       "gpt-5.4",
		requestedAt: time.Now().Add(-1500 * time.Millisecond),
	}

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Latency < time.Second {
		t.Fatalf("latency = %v, want >= 1s", record.Latency)
	}
	if record.Latency > 3*time.Second {
		t.Fatalf("latency = %v, want <= 3s", record.Latency)
	}
}
