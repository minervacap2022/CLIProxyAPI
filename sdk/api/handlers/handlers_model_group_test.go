package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

// ctxWithGinKeyConfigs builds a context that looks like one produced by keyConfigMiddleware.
func ctxWithGinKeyConfigs(kc *internalconfig.APIKeyConfig, mg *internalconfig.ModelGroup) context.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if kc != nil {
		c.Set("apiKeyConfig", kc)
	}
	if mg != nil {
		c.Set("modelGroup", mg)
	}
	return context.WithValue(context.Background(), "gin", c)
}

// modelAwareExecutor is an executor that returns a quota error for models in the quotaModels
// set and a success payload (the model name) for all other models. It supports both Execute
// and ExecuteStream.
type modelAwareExecutor struct {
	mu          sync.Mutex
	calls       []string
	quotaModels map[string]bool
}

func newModelAwareExecutor(quotaModels ...string) *modelAwareExecutor {
	m := &modelAwareExecutor{quotaModels: make(map[string]bool)}
	for _, model := range quotaModels {
		m.quotaModels[model] = true
	}
	return m
}

func (e *modelAwareExecutor) Identifier() string { return "codex" }

func (e *modelAwareExecutor) Execute(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	e.calls = append(e.calls, req.Model)
	e.mu.Unlock()

	if e.quotaModels[req.Model] {
		return coreexecutor.Response{}, &coreauth.Error{
			Code:       "rate_limit",
			Message:    "rate limit exceeded",
			HTTPStatus: http.StatusTooManyRequests,
		}
	}
	return coreexecutor.Response{Payload: []byte(req.Model)}, nil
}

func (e *modelAwareExecutor) ExecuteStream(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, req.Model)
	e.mu.Unlock()

	ch := make(chan coreexecutor.StreamChunk, 1)
	if e.quotaModels[req.Model] {
		ch <- coreexecutor.StreamChunk{
			Err: &coreauth.Error{
				Code:       "rate_limit",
				Message:    "rate limit exceeded",
				HTTPStatus: http.StatusTooManyRequests,
			},
		}
		close(ch)
		return &coreexecutor.StreamResult{Chunks: ch}, nil
	}
	ch <- coreexecutor.StreamChunk{Payload: []byte(req.Model)}
	close(ch)
	return &coreexecutor.StreamResult{Chunks: ch}, nil
}

func (e *modelAwareExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *modelAwareExecutor) CountTokens(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	e.calls = append(e.calls, req.Model)
	e.mu.Unlock()

	if e.quotaModels[req.Model] {
		return coreexecutor.Response{}, &coreauth.Error{
			Code:       "rate_limit",
			Message:    "rate limit exceeded",
			HTTPStatus: http.StatusTooManyRequests,
		}
	}
	return coreexecutor.Response{Payload: []byte(req.Model)}, nil
}

func (e *modelAwareExecutor) HttpRequest(_ context.Context, _ *coreauth.Auth, _ *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "not implemented", HTTPStatus: http.StatusNotImplemented}
}

func (e *modelAwareExecutor) Models() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.calls))
	copy(out, e.calls)
	return out
}

// setupGroupHandler sets up a Manager + BaseAPIHandler with the given executor and registers
// models in the global registry under a single auth entry.
func setupGroupHandler(t *testing.T, exec coreauth.ProviderExecutor, modelIDs ...string) *BaseAPIHandler {
	t.Helper()
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(exec)

	auth := &coreauth.Auth{
		ID:       "group-auth",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{"email": "group@example.com"},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register: %v", err)
	}

	infos := make([]*registry.ModelInfo, len(modelIDs))
	for i, id := range modelIDs {
		infos[i] = &registry.ModelInfo{ID: id}
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, infos)
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	return NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
}

// --- isQuotaExhausted ---

type statusErr struct{ code int }

func (e *statusErr) Error() string      { return http.StatusText(e.code) }
func (e *statusErr) StatusCode() int    { return e.code }

func TestIsQuotaExhausted_TooManyRequests(t *testing.T) {
	if !isQuotaExhausted(&statusErr{http.StatusTooManyRequests}) {
		t.Error("expected 429 to be quota-exhausted")
	}
}

func TestIsQuotaExhausted_PaymentRequired(t *testing.T) {
	if !isQuotaExhausted(&statusErr{http.StatusPaymentRequired}) {
		t.Error("expected 402 to be quota-exhausted")
	}
}

func TestIsQuotaExhausted_Unauthorized(t *testing.T) {
	if !isQuotaExhausted(&statusErr{http.StatusUnauthorized}) {
		t.Error("expected 401 (upstream credentials invalid) to trigger fallover")
	}
}

func TestIsQuotaExhausted_Forbidden(t *testing.T) {
	if !isQuotaExhausted(&statusErr{http.StatusForbidden}) {
		t.Error("expected 403 (upstream credentials lack access) to trigger fallover")
	}
}

func TestIsQuotaExhausted_ServerErrors(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504} {
		if !isQuotaExhausted(&statusErr{code}) {
			t.Errorf("expected %d (upstream transient error) to trigger fallover", code)
		}
	}
}

func TestIsQuotaExhausted_BadRequest(t *testing.T) {
	// 400 must NOT trigger fallover: request is malformed, switching model won't help.
	if isQuotaExhausted(&statusErr{http.StatusBadRequest}) {
		t.Error("expected 400 NOT to trigger fallover")
	}
}

func TestIsQuotaExhausted_NilError(t *testing.T) {
	if isQuotaExhausted(nil) {
		t.Error("expected nil error to not be quota-exhausted")
	}
}

func TestIsQuotaExhausted_PlainError(t *testing.T) {
	if isQuotaExhausted(errors.New("something went wrong")) {
		t.Error("expected plain error to not be quota-exhausted")
	}
}

func TestIsQuotaExhausted_DeadlineExceeded(t *testing.T) {
	// context.DeadlineExceeded means the upstream timed out — treat as failover signal.
	if !isQuotaExhausted(context.DeadlineExceeded) {
		t.Error("expected context.DeadlineExceeded to trigger failover")
	}
}

func TestIsQuotaExhausted_ContextCanceled(t *testing.T) {
	// context.Canceled means the client disconnected — must NOT trigger failover.
	if isQuotaExhausted(context.Canceled) {
		t.Error("expected context.Canceled NOT to trigger failover")
	}
}

// deadlineExecutor returns context.DeadlineExceeded for models in the deadlineModels set.
type deadlineExecutor struct {
	modelAwareExecutor
}

func newDeadlineExecutor(deadlineModels ...string) *deadlineExecutor {
	e := &deadlineExecutor{}
	e.quotaModels = make(map[string]bool)
	for _, m := range deadlineModels {
		e.quotaModels[m] = true
	}
	return e
}

func (e *deadlineExecutor) Execute(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	e.calls = append(e.calls, req.Model)
	e.mu.Unlock()
	if e.quotaModels[req.Model] {
		return coreexecutor.Response{}, context.DeadlineExceeded
	}
	return coreexecutor.Response{Payload: []byte(req.Model)}, nil
}

func (e *deadlineExecutor) ExecuteStream(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, req.Model)
	e.mu.Unlock()
	if e.quotaModels[req.Model] {
		return nil, context.DeadlineExceeded
	}
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{Payload: []byte(req.Model)}
	close(ch)
	return &coreexecutor.StreamResult{Chunks: ch}, nil
}

func TestExecuteWithAuthManager_DeadlineExceeded_Failover(t *testing.T) {
	/*
	 * primary (priority 2) returns context.DeadlineExceeded (upstream timeout).
	 * Expect failover to backup (priority 1).
	 */
	exec := newDeadlineExecutor("primary-model")
	handler := setupGroupHandler(t, exec, "primary-model", "backup-model")

	mg := &internalconfig.ModelGroup{
		Name: "timeout-group",
		Models: []internalconfig.ModelGroupEntry{
			{Model: "primary-model", Priority: 2},
			{Model: "backup-model", Priority: 1},
		},
	}
	kc := &internalconfig.APIKeyConfig{Key: "k", ModelGroup: "timeout-group"}
	ctx := ctxWithGinKeyConfigs(kc, mg)

	payload, _, errMsg := handler.ExecuteWithAuthManager(ctx, "openai", "timeout-group", []byte(`{}`), "")
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg.Error)
	}
	if string(payload) != "backup-model" {
		t.Errorf("expected failover to 'backup-model', got %q", payload)
	}
}

func TestExecuteStreamWithAuthManager_DeadlineExceeded_Failover(t *testing.T) {
	/*
	 * primary (priority 2) returns context.DeadlineExceeded on stream open.
	 * Expect failover to backup (priority 1).
	 */
	exec := newDeadlineExecutor("primary-model")
	handler := setupGroupHandler(t, exec, "primary-model", "backup-model")

	mg := &internalconfig.ModelGroup{
		Name: "stream-timeout-group",
		Models: []internalconfig.ModelGroupEntry{
			{Model: "primary-model", Priority: 2},
			{Model: "backup-model", Priority: 1},
		},
	}
	kc := &internalconfig.APIKeyConfig{Key: "k", ModelGroup: "stream-timeout-group"}
	ctx := ctxWithGinKeyConfigs(kc, mg)

	dataChan, _, errChan := handler.ExecuteStreamWithAuthManager(ctx, "openai", "stream-timeout-group", []byte(`{}`), "")
	var chunks []byte
	for b := range dataChan {
		chunks = append(chunks, b...)
	}
	if err := <-errChan; err != nil {
		t.Fatalf("unexpected error: %v", err.Error)
	}
	if string(chunks) != "backup-model" {
		t.Errorf("expected stream failover to 'backup-model', got %q", chunks)
	}
}

// --- ginKeyConfigs ---

func TestGinKeyConfigs_NoGinContext_ReturnsNils(t *testing.T) {
	kc, mg := ginKeyConfigs(context.Background())
	if kc != nil || mg != nil {
		t.Errorf("expected nil, nil; got %v, %v", kc, mg)
	}
}

func TestGinKeyConfigs_NilCtx_ReturnsNils(t *testing.T) {
	kc, mg := ginKeyConfigs(nil)
	if kc != nil || mg != nil {
		t.Error("expected nil, nil for nil context")
	}
}

func TestGinKeyConfigs_WithKeyConfig(t *testing.T) {
	wantKC := &internalconfig.APIKeyConfig{Key: "k", AllowedModels: []string{"m1"}}
	ctx := ctxWithGinKeyConfigs(wantKC, nil)
	kc, mg := ginKeyConfigs(ctx)
	if kc != wantKC {
		t.Errorf("expected key config %v, got %v", wantKC, kc)
	}
	if mg != nil {
		t.Errorf("expected nil model group, got %v", mg)
	}
}

func TestGinKeyConfigs_WithModelGroup(t *testing.T) {
	wantMG := &internalconfig.ModelGroup{Name: "my-group"}
	ctx := ctxWithGinKeyConfigs(nil, wantMG)
	_, mg := ginKeyConfigs(ctx)
	if mg != wantMG {
		t.Errorf("expected model group %v, got %v", wantMG, mg)
	}
}

// --- ExecuteWithAuthManager model access check ---

func TestExecuteWithAuthManager_DeniedModel_Returns403(t *testing.T) {
	exec := newModelAwareExecutor()
	handler := setupGroupHandler(t, exec, "allowed-model")
	kc := &internalconfig.APIKeyConfig{Key: "k", AllowedModels: []string{"allowed-model"}}
	ctx := ctxWithGinKeyConfigs(kc, nil)

	_, _, errMsg := handler.ExecuteWithAuthManager(ctx, "openai", "disallowed-model", []byte(`{}`), "")
	if errMsg == nil {
		t.Fatal("expected error for disallowed model")
	}
	if errMsg.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", errMsg.StatusCode)
	}
}

func TestExecuteWithAuthManager_AllowedModel_Succeeds(t *testing.T) {
	exec := newModelAwareExecutor()
	handler := setupGroupHandler(t, exec, "allowed-model")
	kc := &internalconfig.APIKeyConfig{Key: "k", AllowedModels: []string{"allowed-model"}}
	ctx := ctxWithGinKeyConfigs(kc, nil)

	payload, _, errMsg := handler.ExecuteWithAuthManager(ctx, "openai", "allowed-model", []byte(`{}`), "")
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg.Error)
	}
	if string(payload) != "allowed-model" {
		t.Errorf("expected payload 'allowed-model', got %q", payload)
	}
}

func TestExecuteWithAuthManager_NoKeyConfig_AllowsAll(t *testing.T) {
	exec := newModelAwareExecutor()
	handler := setupGroupHandler(t, exec, "any-model")

	// No gin context at all — backward compatible.
	payload, _, errMsg := handler.ExecuteWithAuthManager(context.Background(), "openai", "any-model", []byte(`{}`), "")
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg.Error)
	}
	if string(payload) != "any-model" {
		t.Errorf("expected payload 'any-model', got %q", payload)
	}
}

// --- executeWithModelGroup ---

func TestExecuteWithAuthManager_ModelGroupFallback(t *testing.T) {
	/*
	 * Model group has two tiers:
	 *   - priority 2: "quota-model" (always returns 429)
	 *   - priority 1: "fallback-model" (succeeds)
	 * Requesting the group name should fall through to "fallback-model".
	 */
	exec := newModelAwareExecutor("quota-model")
	handler := setupGroupHandler(t, exec, "quota-model", "fallback-model")

	mg := &internalconfig.ModelGroup{
		Name: "my-group",
		Models: []internalconfig.ModelGroupEntry{
			{Model: "quota-model", Priority: 2},
			{Model: "fallback-model", Priority: 1},
		},
	}
	kc := &internalconfig.APIKeyConfig{Key: "k", ModelGroup: "my-group"}
	ctx := ctxWithGinKeyConfigs(kc, mg)

	payload, _, errMsg := handler.ExecuteWithAuthManager(ctx, "openai", "my-group", []byte(`{}`), "")
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg.Error)
	}
	if string(payload) != "fallback-model" {
		t.Errorf("expected payload 'fallback-model', got %q", payload)
	}
}

func TestExecuteWithAuthManager_ModelGroupAllExhausted_Returns429(t *testing.T) {
	exec := newModelAwareExecutor("model-a", "model-b")
	handler := setupGroupHandler(t, exec, "model-a", "model-b")

	mg := &internalconfig.ModelGroup{
		Name: "all-quota",
		Models: []internalconfig.ModelGroupEntry{
			{Model: "model-a", Priority: 2},
			{Model: "model-b", Priority: 1},
		},
	}
	kc := &internalconfig.APIKeyConfig{Key: "k", ModelGroup: "all-quota"}
	ctx := ctxWithGinKeyConfigs(kc, mg)

	_, _, errMsg := handler.ExecuteWithAuthManager(ctx, "openai", "all-quota", []byte(`{}`), "")
	if errMsg == nil {
		t.Fatal("expected error when all models exhausted")
	}
	if errMsg.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", errMsg.StatusCode)
	}
}

func TestExecuteWithAuthManager_ModelGroupSameTierFallback(t *testing.T) {
	/*
	 * Two models in the SAME tier. First one is quota-exhausted; second should succeed.
	 */
	exec := newModelAwareExecutor("model-first")
	handler := setupGroupHandler(t, exec, "model-first", "model-second")

	mg := &internalconfig.ModelGroup{
		Name: "same-tier",
		Models: []internalconfig.ModelGroupEntry{
			{Model: "model-first", Priority: 1},
			{Model: "model-second", Priority: 1},
		},
	}
	kc := &internalconfig.APIKeyConfig{Key: "k", ModelGroup: "same-tier"}
	ctx := ctxWithGinKeyConfigs(kc, mg)

	payload, _, errMsg := handler.ExecuteWithAuthManager(ctx, "openai", "same-tier", []byte(`{}`), "")
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg.Error)
	}
	if string(payload) != "model-second" {
		t.Errorf("expected payload 'model-second', got %q", payload)
	}
}

// --- ExecuteStreamWithAuthManager model group ---

func TestExecuteStreamWithAuthManager_ModelGroupFallback(t *testing.T) {
	exec := newModelAwareExecutor("quota-model")
	handler := setupGroupHandler(t, exec, "quota-model", "fallback-model")

	mg := &internalconfig.ModelGroup{
		Name: "my-stream-group",
		Models: []internalconfig.ModelGroupEntry{
			{Model: "quota-model", Priority: 2},
			{Model: "fallback-model", Priority: 1},
		},
	}
	kc := &internalconfig.APIKeyConfig{Key: "k", ModelGroup: "my-stream-group"}
	ctx := ctxWithGinKeyConfigs(kc, mg)

	dataChan, _, errChan := handler.ExecuteStreamWithAuthManager(ctx, "openai", "my-stream-group", []byte(`{}`), "")
	if dataChan == nil || errChan == nil {
		t.Fatal("expected non-nil channels")
	}

	var got []byte
	for chunk := range dataChan {
		got = append(got, chunk...)
	}
	for msg := range errChan {
		if msg != nil {
			t.Fatalf("unexpected error: %v", msg.Error)
		}
	}

	if string(got) != "fallback-model" {
		t.Errorf("expected payload 'fallback-model', got %q", got)
	}
}

func TestExecuteStreamWithAuthManager_ModelGroupAllExhausted_Returns429(t *testing.T) {
	exec := newModelAwareExecutor("model-a", "model-b")
	handler := setupGroupHandler(t, exec, "model-a", "model-b")

	mg := &internalconfig.ModelGroup{
		Name: "stream-all-quota",
		Models: []internalconfig.ModelGroupEntry{
			{Model: "model-a", Priority: 1},
			{Model: "model-b", Priority: 1},
		},
	}
	kc := &internalconfig.APIKeyConfig{Key: "k", ModelGroup: "stream-all-quota"}
	ctx := ctxWithGinKeyConfigs(kc, mg)

	dataChan, _, errChan := handler.ExecuteStreamWithAuthManager(ctx, "openai", "stream-all-quota", []byte(`{}`), "")

	for range dataChan {
	}
	var statusCode int
	for msg := range errChan {
		if msg != nil {
			statusCode = msg.StatusCode
		}
	}
	if statusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", statusCode)
	}
}

func TestExecuteStreamWithAuthManager_DeniedModel_Returns403(t *testing.T) {
	exec := newModelAwareExecutor()
	handler := setupGroupHandler(t, exec, "allowed-model")
	kc := &internalconfig.APIKeyConfig{Key: "k", AllowedModels: []string{"allowed-model"}}
	ctx := ctxWithGinKeyConfigs(kc, nil)

	// dataChan is nil for immediate error responses (matching existing getRequestDetails error paths).
	dataChan, _, errChan := handler.ExecuteStreamWithAuthManager(ctx, "openai", "bad-model", []byte(`{}`), "")

	// Guard: nil dataChan is expected here — ranging over nil blocks forever.
	for dataChan != nil {
		chunk, ok := <-dataChan
		if !ok {
			break
		}
		_ = chunk
	}
	var statusCode int
	for msg := range errChan {
		if msg != nil {
			statusCode = msg.StatusCode
		}
	}
	if statusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", statusCode)
	}
}
