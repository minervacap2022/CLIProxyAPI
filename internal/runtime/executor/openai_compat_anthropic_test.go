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

func anthropicCompatTestConfig(baseURL, authType string) *config.Config {
	return &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
		Name:     "command-code",
		Protocol: "anthropic",
		AuthType: authType,
		BaseURL:  baseURL,
	}}}
}

func anthropicCompatTestAuth(baseURL string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url":         baseURL,
		"api_key":          "test-key",
		"compat_name":      "command-code",
		"provider_key":     "command-code",
		"compat_protocol":  "anthropic",
		"compat_auth_type": "bearer",
	}}
}

func TestOpenAICompatExecutorAnthropicRoutesAndTranslatesNonStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider/v1/messages" {
			t.Fatalf("path = %q, want /provider/v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Fatalf("x-api-key = %q, want empty for bearer auth", got)
		}
		if got := r.Header.Get("Anthropic-Version"); got != openAICompatAnthropicVersion {
			t.Fatalf("Anthropic-Version = %q, want %q", got, openAICompatAnthropicVersion)
		}
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Fatalf("read request body: %v", errRead)
		}
		if got := gjson.GetBytes(body, "model").String(); got != "claude-command-code" {
			t.Fatalf("model = %q, want claude-command-code", got)
		}
		if got := gjson.GetBytes(body, "max_tokens").Int(); got != 8 {
			t.Fatalf("max_tokens = %d, want 8", got)
		}
		if got := gjson.GetBytes(body, "messages.0.content.0.text").String(); got != "Hi" {
			t.Fatalf("message content = %q, want Hi", got)
		}
		if !gjson.GetBytes(body, "stream").Bool() {
			t.Fatal("stream = false, want true for OpenAI response adaptation")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-command-code\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello from Anthropic\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n"))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", anthropicCompatTestConfig(server.URL+"/provider/v1", "bearer"))
	resp, err := executor.Execute(context.Background(), anthropicCompatTestAuth(server.URL+"/provider/v1"), cliproxyexecutor.Request{
		Model:   "claude-command-code",
		Payload: []byte(`{"model":"claude-command-code","messages":[{"role":"user","content":"Hi"}],"max_tokens":8}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := gjson.GetBytes(resp.Payload, "choices.0.message.content").String(); got != "Hello from Anthropic" {
		t.Fatalf("OpenAI response content = %q, want translated Anthropic content", got)
	}
}

func TestOpenAICompatExecutorAnthropicStreamsWithAPIKeyAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider/v1/messages" {
			t.Fatalf("path = %q, want /provider/v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty for x-api-key auth", got)
		}
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Fatalf("read request body: %v", errRead)
		}
		if !gjson.GetBytes(body, "stream").Bool() {
			t.Fatal("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-command-code\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"streamed response\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	auth := anthropicCompatTestAuth(server.URL + "/provider/v1")
	auth.Attributes["compat_auth_type"] = "x-api-key"
	executor := NewOpenAICompatExecutor("openai-compatibility", anthropicCompatTestConfig(server.URL+"/provider/v1", "x-api-key"))
	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-command-code",
		Payload: []byte(`{"model":"claude-command-code","messages":[{"role":"user","content":"Hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream returned error: %v", err)
	}

	var payload strings.Builder
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		payload.Write(chunk.Payload)
	}
	if !strings.Contains(payload.String(), "streamed response") {
		t.Fatalf("translated stream = %q, want OpenAI chunk containing response text", payload.String())
	}
}
