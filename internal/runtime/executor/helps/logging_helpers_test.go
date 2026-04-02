package helps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestRecordAPIWebsocketHandshakeDoesNotMarkAPIResponseTimestamp(t *testing.T) {
	t.Parallel()

	ctx, ginCtx := newLoggingTestContext()
	cfg := &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}

	RecordAPIWebsocketHandshake(ctx, cfg, http.StatusSwitchingProtocols, http.Header{
		"Connection": {"Upgrade"},
		"Upgrade":    {"websocket"},
	})

	if _, exists := ginCtx.Get("API_RESPONSE_TIMESTAMP"); exists {
		t.Fatal("expected websocket handshake to leave API_RESPONSE_TIMESTAMP unset")
	}

	timeline, exists := ginCtx.Get(apiWebsocketTimelineKey)
	if !exists {
		t.Fatal("expected websocket handshake timeline entry")
	}
	timelineBytes, ok := timeline.([]byte)
	if !ok {
		t.Fatalf("timeline type = %T, want []byte", timeline)
	}
	if !strings.Contains(string(timelineBytes), "Event: api.websocket.handshake") {
		t.Fatalf("handshake timeline missing event: %s", string(timelineBytes))
	}
}

func TestRecordAPIWebsocketUpgradeRejectionStoresHTTPAttempt(t *testing.T) {
	t.Parallel()

	ctx, ginCtx := newLoggingTestContext()
	cfg := &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}

	RecordAPIWebsocketRequest(ctx, cfg, UpstreamRequestLog{
		URL:     "wss://example.com/backend-api/codex/responses",
		Method:  "WEBSOCKET",
		Headers: http.Header{"Authorization": {"Bearer ws-token"}},
		Body:    []byte(`{"type":"response.create"}`),
	})
	RecordAPIWebsocketUpgradeRejection(ctx, cfg, UpstreamRequestLog{
		URL:     "https://example.com/backend-api/codex/responses",
		Method:  http.MethodGet,
		Headers: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}},
	}, http.StatusUpgradeRequired, http.Header{
		"Content-Type": {"application/json"},
	}, []byte(`{"error":"upgrade required"}`))

	if _, exists := ginCtx.Get(apiWebsocketTimelineKey); exists {
		t.Fatal("expected rejected websocket upgrade to clear websocket timeline")
	}
	if _, exists := ginCtx.Get("API_RESPONSE_TIMESTAMP"); exists {
		t.Fatal("expected rejected websocket upgrade not to claim API_RESPONSE_TIMESTAMP")
	}

	apiRequest, exists := ginCtx.Get(apiRequestKey)
	if !exists {
		t.Fatal("expected HTTP API request log for rejected websocket upgrade")
	}
	apiRequestText, ok := apiRequest.([]byte)
	if !ok {
		t.Fatalf("apiRequest type = %T, want []byte", apiRequest)
	}
	requestText := string(apiRequestText)
	if !strings.Contains(requestText, "=== API REQUEST 1 ===") {
		t.Fatalf("expected first HTTP attempt to be recorded: %s", requestText)
	}
	if !strings.Contains(requestText, "HTTP Method: GET") {
		t.Fatalf("expected rejected upgrade to be logged as GET: %s", requestText)
	}
	if strings.Contains(requestText, "<missing>") {
		t.Fatalf("expected rejected upgrade request to avoid placeholder request entry: %s", requestText)
	}

	apiResponse, exists := ginCtx.Get(apiResponseKey)
	if !exists {
		t.Fatal("expected HTTP API response log for rejected websocket upgrade")
	}
	apiResponseText, ok := apiResponse.([]byte)
	if !ok {
		t.Fatalf("apiResponse type = %T, want []byte", apiResponse)
	}
	responseText := string(apiResponseText)
	if !strings.Contains(responseText, "=== API RESPONSE 1 ===") {
		t.Fatalf("expected first HTTP response attempt to be recorded: %s", responseText)
	}
	if !strings.Contains(responseText, "Status: 426") {
		t.Fatalf("expected rejected upgrade status to be logged: %s", responseText)
	}
	if !strings.Contains(responseText, `{"error":"upgrade required"}`) {
		t.Fatalf("expected rejected upgrade body to be logged: %s", responseText)
	}

	RecordAPIRequest(ctx, cfg, UpstreamRequestLog{
		URL:    "https://example.com/backend-api/codex/responses",
		Method: http.MethodPost,
		Body:   []byte(`{"model":"gpt-5-codex"}`),
	})
	RecordAPIResponseMetadata(ctx, cfg, http.StatusOK, http.Header{"Content-Type": {"text/event-stream"}})

	apiRequest, _ = ginCtx.Get(apiRequestKey)
	requestText = string(apiRequest.([]byte))
	if !strings.Contains(requestText, "=== API REQUEST 2 ===") {
		t.Fatalf("expected fallback HTTP request to become attempt 2: %s", requestText)
	}

	apiResponse, _ = ginCtx.Get(apiResponseKey)
	responseText = string(apiResponse.([]byte))
	if !strings.Contains(responseText, "=== API RESPONSE 2 ===") {
		t.Fatalf("expected fallback HTTP response to become attempt 2: %s", responseText)
	}
}

func newLoggingTestContext() (context.Context, *gin.Context) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return context.WithValue(context.Background(), "gin", ginCtx), ginCtx
}
