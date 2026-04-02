package logging

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
)

func TestWriteNonStreamingLog_WebsocketTimelineOmitsRequestAndResponseSections(t *testing.T) {
	logger := &FileRequestLogger{}
	var out bytes.Buffer
	timestamp := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)

	err := logger.writeNonStreamingLog(
		&out,
		"/v1/responses",
		"GET",
		map[string][]string{"Upgrade": {"websocket"}},
		[]byte("request-body"),
		"",
		[]byte("Timestamp: 2026-04-01T12:00:00Z\nEvent: websocket.request\n{\"type\":\"response.create\"}\n"),
		nil,
		nil,
		nil,
		nil,
		101,
		map[string][]string{"Connection": {"Upgrade"}},
		[]byte("response-body"),
		nil,
		timestamp,
		time.Time{},
	)
	if err != nil {
		t.Fatalf("writeNonStreamingLog error: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "=== REQUEST BODY ===") {
		t.Fatalf("websocket log should omit request body section: %s", got)
	}
	if strings.Contains(got, "=== RESPONSE ===") {
		t.Fatalf("websocket log should omit response section: %s", got)
	}
	if !strings.Contains(got, "=== WEBSOCKET TIMELINE ===") {
		t.Fatalf("websocket log should include timeline section: %s", got)
	}
	if !strings.Contains(got, "Event: websocket.request") {
		t.Fatalf("websocket timeline event missing: %s", got)
	}
}

func TestWriteNonStreamingLog_HTTPKeepsRequestAndResponseSections(t *testing.T) {
	logger := &FileRequestLogger{}
	var out bytes.Buffer
	timestamp := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)

	err := logger.writeNonStreamingLog(
		&out,
		"/v1/chat/completions",
		"POST",
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"input":"hello"}`),
		"",
		nil,
		nil,
		nil,
		nil,
		nil,
		200,
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"output":"ok"}`),
		nil,
		timestamp,
		time.Time{},
	)
	if err != nil {
		t.Fatalf("writeNonStreamingLog error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "=== REQUEST BODY ===") {
		t.Fatalf("http log should include request body section: %s", got)
	}
	if !strings.Contains(got, "=== RESPONSE ===") {
		t.Fatalf("http log should include response section: %s", got)
	}
}

func TestWriteResponseSection_PreservesLeadingCRLFInBody(t *testing.T) {
	var out bytes.Buffer

	err := writeResponseSection(
		&out,
		200,
		true,
		map[string][]string{"Content-Type": {"text/plain"}},
		strings.NewReader("\r\nhello"),
		nil,
		true,
	)
	if err != nil {
		t.Fatalf("writeResponseSection error: %v", err)
	}

	got := out.String()
	want := "=== RESPONSE ===\nStatus: 200\nContent-Type: text/plain\n\r\nhello\n"
	if got != want {
		t.Fatalf("writeResponseSection output = %q, want %q", got, want)
	}
}

func TestWriteNonStreamingLog_APIWebsocketTimelineUsesDedicatedSection(t *testing.T) {
	logger := &FileRequestLogger{}
	var out bytes.Buffer
	timestamp := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)

	err := logger.writeNonStreamingLog(
		&out,
		"/v1/responses",
		"GET",
		map[string][]string{"Upgrade": {"websocket"}},
		[]byte("request-body"),
		"",
		[]byte("Timestamp: 2026-04-01T12:00:00Z\nEvent: websocket.request\n{\"type\":\"response.create\"}\n"),
		nil,
		nil,
		[]byte("Timestamp: 2026-04-01T12:00:00Z\nEvent: api.websocket.request\n{\"type\":\"response.create\"}\n"),
		nil,
		101,
		map[string][]string{"Connection": {"Upgrade"}},
		[]byte("response-body"),
		nil,
		timestamp,
		time.Time{},
	)
	if err != nil {
		t.Fatalf("writeNonStreamingLog error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "=== API WEBSOCKET TIMELINE ===") {
		t.Fatalf("expected API websocket timeline section: %s", got)
	}
	if !strings.Contains(got, "Upstream Transport: websocket") {
		t.Fatalf("expected upstream websocket transport: %s", got)
	}
	if !strings.Contains(got, "Downstream Transport: websocket") {
		t.Fatalf("expected downstream websocket transport: %s", got)
	}
}

func TestInferUpstreamTransport_DoesNotTreatAPIErrorsAsHTTP(t *testing.T) {
	apiResponseErrors := []*interfaces.ErrorMessage{{StatusCode: 500}}

	if got := inferUpstreamTransport(nil, nil, nil, apiResponseErrors); got != "" {
		t.Fatalf("inferUpstreamTransport() = %q, want empty transport", got)
	}
}

func TestInferUpstreamTransport_PrefersWebsocketWhenOnlyTimelineAndErrorsExist(t *testing.T) {
	apiResponseErrors := []*interfaces.ErrorMessage{{StatusCode: 500}}
	apiWebsocketTimeline := []byte("Timestamp: 2026-04-01T12:00:00Z\nEvent: api.websocket.error\nboom\n")

	if got := inferUpstreamTransport(nil, nil, apiWebsocketTimeline, apiResponseErrors); got != "websocket" {
		t.Fatalf("inferUpstreamTransport() = %q, want %q", got, "websocket")
	}
}

func TestFileStreamingLogWriter_WriteFinalLogIncludesAPIWebsocketTimeline(t *testing.T) {
	logger := NewFileRequestLogger(true, t.TempDir(), "", 0)

	streamWriter, err := logger.LogStreamingRequest(
		"/v1/responses",
		"POST",
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"model":"gpt-5-codex","stream":true}`),
		"stream-websocket",
	)
	if err != nil {
		t.Fatalf("LogStreamingRequest error: %v", err)
	}

	writer, ok := streamWriter.(*FileStreamingLogWriter)
	if !ok {
		t.Fatalf("stream writer type = %T, want *FileStreamingLogWriter", streamWriter)
	}

	if err = writer.WriteStatus(200, map[string][]string{"Content-Type": {"text/event-stream"}}); err != nil {
		t.Fatalf("WriteStatus error: %v", err)
	}
	if err = writer.WriteAPIWebsocketTimeline([]byte("Timestamp: 2026-04-01T12:00:00Z\nEvent: api.websocket.request\n{\"type\":\"response.create\"}\n")); err != nil {
		t.Fatalf("WriteAPIWebsocketTimeline error: %v", err)
	}
	writer.WriteChunkAsync([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	writer.SetFirstChunkTimestamp(time.Date(2026, time.April, 1, 12, 0, 1, 0, time.UTC))

	if err = writer.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	got, err := os.ReadFile(writer.logFilePath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	logText := string(got)
	if !strings.Contains(logText, "=== API WEBSOCKET TIMELINE ===") {
		t.Fatalf("streaming log missing API websocket timeline section: %s", logText)
	}
	if !strings.Contains(logText, "Event: api.websocket.request") {
		t.Fatalf("streaming log missing websocket timeline event: %s", logText)
	}
	if !strings.Contains(logText, "Upstream Transport: websocket") {
		t.Fatalf("streaming log missing websocket upstream transport: %s", logText)
	}
}
