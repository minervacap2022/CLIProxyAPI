package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestClaudeExecutor_StripsUnsignedThinkingBlocks verifies the integration-level
// behavior of the Kimi→Opus regression fix: when a Claude request arrives with
// an assistant history containing a thinking block without a signature (the shape
// Kimi produces via the openai→claude translator), ClaudeExecutor must drop that
// block before forwarding to Anthropic. Otherwise Anthropic returns
// 400 "Invalid `signature` in `thinking` block".
func TestClaudeExecutor_StripsUnsignedThinkingBlocks(t *testing.T) {
	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"claude-3-5-sonnet","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	// Assistant message mirrors Kimi's openai→claude translator output:
	// a thinking block with no signature field, followed by plain text.
	payload := []byte(`{
		"model":"claude-3-5-sonnet",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"ping"}]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"kimi reasoning leaked through"},
				{"type":"text","text":"pong"}
			]},
			{"role":"user","content":[{"type":"text","text":"continue"}]}
		]
	}`)

	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3-5-sonnet",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	}); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(upstreamBody) == 0 {
		t.Fatal("upstream server did not receive a request body")
	}

	// The unsigned thinking block must be gone before it hits Anthropic.
	if strings.Contains(string(upstreamBody), "kimi reasoning leaked through") {
		t.Fatalf("expected unsigned thinking block to be stripped; upstream body:\n%s", upstreamBody)
	}

	// Surrounding turns must survive.
	if !strings.Contains(string(upstreamBody), `"pong"`) {
		t.Fatalf("expected assistant text to remain, got:\n%s", upstreamBody)
	}
	if !strings.Contains(string(upstreamBody), `"continue"`) {
		t.Fatalf("expected trailing user text to remain, got:\n%s", upstreamBody)
	}

	// And the assistant message itself must still be present (only its
	// thinking block was removed, not the whole turn).
	roles := gjson.GetBytes(upstreamBody, "messages.#.role").Array()
	var assistantCount int
	for _, r := range roles {
		if r.String() == "assistant" {
			assistantCount++
		}
	}
	if assistantCount != 1 {
		t.Fatalf("expected exactly one assistant message, got %d; upstream body:\n%s", assistantCount, upstreamBody)
	}
}

// TestClaudeExecutor_PreservesSignedThinkingBlocks verifies the guard does not
// over-reach: a real Anthropic-signed thinking block from a prior Claude turn
// must be forwarded unchanged so multi-turn extended thinking keeps working.
func TestClaudeExecutor_PreservesSignedThinkingBlocks(t *testing.T) {
	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"claude-3-5-sonnet","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	executor := NewClaudeExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"api_key":  "key-123",
		"base_url": server.URL,
	}}

	// A signature long enough to satisfy any minimum-length check elsewhere.
	validSig := strings.Repeat("a", 128)
	payload := []byte(`{
		"model":"claude-3-5-sonnet",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"q"}]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"anthropic signed thought","signature":"` + validSig + `"},
				{"type":"text","text":"a"}
			]},
			{"role":"user","content":[{"type":"text","text":"follow up"}]}
		]
	}`)

	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-3-5-sonnet",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	}); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if !strings.Contains(string(upstreamBody), "anthropic signed thought") {
		t.Fatalf("expected signed thinking block to be preserved, got:\n%s", upstreamBody)
	}
	if !strings.Contains(string(upstreamBody), validSig) {
		t.Fatalf("expected signature to be preserved, got:\n%s", upstreamBody)
	}
}
