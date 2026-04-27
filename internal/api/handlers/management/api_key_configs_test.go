package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func newTestHandlerWithConfig(t *testing.T, cfg *config.Config) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	h := &Handler{cfg: cfg, configFilePath: cfgPath}
	return h, cfgPath
}

// doRequest calls a gin handler with the given JSON body and returns the response recorder.
func doRequest(t *testing.T, handler gin.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	return w
}

// doRequestWithQuery calls a gin handler with no body but query params.
func doRequestWithQuery(t *testing.T, handler gin.HandlerFunc, method, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/?"+query, nil)
	handler(c)
	return w
}

// --- GetAPIKeyConfigs ---

func TestGetAPIKeyConfigs_ReturnsEmptyList(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{SDKConfig: sdkconfig.SDKConfig{}})
	w := doRequest(t, h.GetAPIKeyConfigs, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["api-key-configs"]; !ok {
		t.Error("response missing api-key-configs key")
	}
}

func TestGetAPIKeyConfigs_ReturnsExistingEntries(t *testing.T) {
	cfg := &config.Config{
		APIKeyConfigs: []config.APIKeyConfig{
			{Key: "k1", Label: "first"},
			{Key: "k2", AllowedModels: []string{"model-a"}},
		},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	w := doRequest(t, h.GetAPIKeyConfigs, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		APIKeyConfigs []config.APIKeyConfig `json:"api-key-configs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.APIKeyConfigs) != 2 {
		t.Errorf("expected 2 entries, got %d", len(resp.APIKeyConfigs))
	}
}

// --- PutAPIKeyConfigs ---

func TestPutAPIKeyConfigs_ReplacesAll(t *testing.T) {
	cfg := &config.Config{
		APIKeyConfigs: []config.APIKeyConfig{{Key: "old"}},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	body := `{"api-key-configs":[{"key":"new1"},{"key":"new2"}]}`
	w := doRequest(t, h.PutAPIKeyConfigs, http.MethodPut, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.APIKeyConfigs) != 2 {
		t.Errorf("expected 2 entries after PUT, got %d", len(h.cfg.APIKeyConfigs))
	}
	if h.cfg.APIKeyConfigs[0].Key != "new1" {
		t.Errorf("expected first key 'new1', got %q", h.cfg.APIKeyConfigs[0].Key)
	}
}

func TestPutAPIKeyConfigs_SyncsAPIKeysFlatList(t *testing.T) {
	cfg := &config.Config{}
	h, _ := newTestHandlerWithConfig(t, cfg)
	body := `{"api-key-configs":[{"key":"flat-key"}]}`
	doRequest(t, h.PutAPIKeyConfigs, http.MethodPut, body)
	found := false
	for _, k := range h.cfg.APIKeys {
		if k == "flat-key" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'flat-key' to appear in flat api-keys list after PUT, got %v", h.cfg.APIKeys)
	}
}

func TestPutAPIKeyConfigs_InvalidBody_Returns400(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	w := doRequest(t, h.PutAPIKeyConfigs, http.MethodPut, "not-json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- PatchAPIKeyConfig ---

func TestPatchAPIKeyConfig_InsertsNewEntry(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	body := `{"value":{"key":"k1","label":"new"}}`
	w := doRequest(t, h.PatchAPIKeyConfig, http.MethodPatch, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.APIKeyConfigs) != 1 || h.cfg.APIKeyConfigs[0].Key != "k1" {
		t.Errorf("expected one entry with key 'k1', got %v", h.cfg.APIKeyConfigs)
	}
}

func TestPatchAPIKeyConfig_UpdatesExistingEntry(t *testing.T) {
	cfg := &config.Config{
		APIKeyConfigs: []config.APIKeyConfig{{Key: "k1", Label: "old"}},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	body := `{"value":{"key":"k1","label":"updated","allowed-models":["m1"]}}`
	w := doRequest(t, h.PatchAPIKeyConfig, http.MethodPatch, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.APIKeyConfigs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(h.cfg.APIKeyConfigs))
	}
	if h.cfg.APIKeyConfigs[0].Label != "updated" {
		t.Errorf("expected label 'updated', got %q", h.cfg.APIKeyConfigs[0].Label)
	}
	if len(h.cfg.APIKeyConfigs[0].AllowedModels) != 1 || h.cfg.APIKeyConfigs[0].AllowedModels[0] != "m1" {
		t.Errorf("unexpected allowed-models: %v", h.cfg.APIKeyConfigs[0].AllowedModels)
	}
}

func TestPatchAPIKeyConfig_MissingKey_Returns400(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	body := `{"value":{"label":"no-key"}}`
	w := doRequest(t, h.PatchAPIKeyConfig, http.MethodPatch, body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestPatchAPIKeyConfig_SyncsAPIKeysFlatList(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	body := `{"value":{"key":"patched-key"}}`
	doRequest(t, h.PatchAPIKeyConfig, http.MethodPatch, body)
	found := false
	for _, k := range h.cfg.APIKeys {
		if k == "patched-key" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'patched-key' in flat api-keys list, got %v", h.cfg.APIKeys)
	}
}

// --- DeleteAPIKeyConfig ---

func TestDeleteAPIKeyConfig_RemovesMatchingEntry(t *testing.T) {
	cfg := &config.Config{
		APIKeyConfigs: []config.APIKeyConfig{
			{Key: "keep"},
			{Key: "remove"},
		},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	w := doRequestWithQuery(t, h.DeleteAPIKeyConfig, http.MethodDelete, "key=remove")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.APIKeyConfigs) != 1 || h.cfg.APIKeyConfigs[0].Key != "keep" {
		t.Errorf("unexpected entries after delete: %v", h.cfg.APIKeyConfigs)
	}
}

func TestDeleteAPIKeyConfig_MissingQueryParam_Returns400(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	w := doRequestWithQuery(t, h.DeleteAPIKeyConfig, http.MethodDelete, "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteAPIKeyConfig_NonExistentKey_NoOp(t *testing.T) {
	cfg := &config.Config{
		APIKeyConfigs: []config.APIKeyConfig{{Key: "existing"}},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	w := doRequestWithQuery(t, h.DeleteAPIKeyConfig, http.MethodDelete, "key=nonexistent")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(h.cfg.APIKeyConfigs) != 1 {
		t.Errorf("expected 1 entry after no-op delete, got %d", len(h.cfg.APIKeyConfigs))
	}
}

// --- keyConfigRefreshFunc callback ---

func TestPutAPIKeyConfigs_CallsRefreshFunc(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	called := false
	h.SetKeyConfigRefreshFunc(func() { called = true })
	doRequest(t, h.PutAPIKeyConfigs, http.MethodPut, `{"api-key-configs":[{"key":"k"}]}`)
	if !called {
		t.Error("expected refresh func to be called after PUT")
	}
}

func TestPatchAPIKeyConfig_CallsRefreshFunc(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	called := false
	h.SetKeyConfigRefreshFunc(func() { called = true })
	doRequest(t, h.PatchAPIKeyConfig, http.MethodPatch, `{"value":{"key":"k"}}`)
	if !called {
		t.Error("expected refresh func to be called after PATCH")
	}
}

func TestDeleteAPIKeyConfig_CallsRefreshFunc(t *testing.T) {
	cfg := &config.Config{APIKeyConfigs: []config.APIKeyConfig{{Key: "k"}}}
	h, _ := newTestHandlerWithConfig(t, cfg)
	called := false
	h.SetKeyConfigRefreshFunc(func() { called = true })
	doRequestWithQuery(t, h.DeleteAPIKeyConfig, http.MethodDelete, "key=k")
	if !called {
		t.Error("expected refresh func to be called after DELETE")
	}
}

