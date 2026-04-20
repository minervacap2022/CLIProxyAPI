package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

type fakeWarmupCtrl struct {
	reloadErr    error
	triggered    []string
	reloadCalls  int
	providerList []string
}

func (f *fakeWarmupCtrl) TriggerNow(_ context.Context, reason string) {
	f.triggered = append(f.triggered, reason)
}
func (f *fakeWarmupCtrl) Reload() error {
	f.reloadCalls++
	return f.reloadErr
}
func (f *fakeWarmupCtrl) SupportedProviders() []string { return f.providerList }

func newWarmupTestHandler(t *testing.T, initial config.WarmupConfig) (*Handler, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8317\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return &Handler{
		cfg:            &config.Config{Port: 8317, Warmup: initial},
		configFilePath: path,
	}, path
}

func TestGetWarmup_IncludesSupportedProviders(t *testing.T) {
	h, _ := newWarmupTestHandler(t, config.WarmupConfig{Enabled: true, Interval: "1h"})
	ctrl := &fakeWarmupCtrl{providerList: []string{"claude", "gemini"}}
	h.SetWarmupController(ctrl)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.GetWarmup(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Warmup             config.WarmupConfig `json:"warmup"`
		SupportedProviders []string            `json:"supported_providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Warmup.Enabled {
		t.Error("expected warmup enabled in response")
	}
	if len(body.SupportedProviders) != 2 {
		t.Errorf("supported providers = %v, want 2 entries", body.SupportedProviders)
	}
}

func TestPutWarmup_UpdatesConfigAndReloads(t *testing.T) {
	h, _ := newWarmupTestHandler(t, config.WarmupConfig{})
	ctrl := &fakeWarmupCtrl{}
	h.SetWarmupController(ctrl)

	body := []byte(`{"enabled":true,"interval":"1h","start-at":"08:50","on-startup":true}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/warmup", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.PutWarmup(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !h.cfg.Warmup.Enabled || h.cfg.Warmup.Interval != "1h" || h.cfg.Warmup.StartAt != "08:50" {
		t.Errorf("config not updated: %+v", h.cfg.Warmup)
	}
	if ctrl.reloadCalls != 1 {
		t.Errorf("expected one reload call, got %d", ctrl.reloadCalls)
	}
}

func TestPutWarmup_RollsBackOnReloadError(t *testing.T) {
	original := config.WarmupConfig{Enabled: true, Interval: "2h"}
	h, _ := newWarmupTestHandler(t, original)
	ctrl := &fakeWarmupCtrl{reloadErr: errBadOption("interval: too small")}
	h.SetWarmupController(ctrl)

	body := []byte(`{"enabled":true,"interval":"1s"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/warmup", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.PutWarmup(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if h.cfg.Warmup.Interval != "2h" {
		t.Errorf("config not rolled back: %+v", h.cfg.Warmup)
	}
}

func TestTriggerWarmup_NoControllerReturns503(t *testing.T) {
	h, _ := newWarmupTestHandler(t, config.WarmupConfig{})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/warmup/trigger", nil)
	h.TriggerWarmup(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

// errBadOption is a simple error sentinel usable as error type in tests
// without pulling the warmup package into the management test package.
type errBadOption string

func (e errBadOption) Error() string { return string(e) }
