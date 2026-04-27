package management

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// --- GetModelGroups ---

func TestGetModelGroups_ReturnsEmptyList(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	w := doRequest(t, h.GetModelGroups, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["model-groups"]; !ok {
		t.Error("response missing model-groups key")
	}
}

func TestGetModelGroups_ReturnsExistingEntries(t *testing.T) {
	cfg := &config.Config{
		ModelGroups: []config.ModelGroup{
			{Name: "group-a", Models: []config.ModelGroupEntry{{Model: "m1", Priority: 1}}},
		},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	w := doRequest(t, h.GetModelGroups, http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		ModelGroups []config.ModelGroup `json:"model-groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.ModelGroups) != 1 || resp.ModelGroups[0].Name != "group-a" {
		t.Errorf("unexpected model groups: %v", resp.ModelGroups)
	}
}

// --- PutModelGroups ---

func TestPutModelGroups_ReplacesAll(t *testing.T) {
	cfg := &config.Config{
		ModelGroups: []config.ModelGroup{{Name: "old"}},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	body := `{"model-groups":[{"name":"new-a","models":[{"model":"m1","priority":1}]},{"name":"new-b","models":[{"model":"m2","priority":2}]}]}`
	w := doRequest(t, h.PutModelGroups, http.MethodPut, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.ModelGroups) != 2 {
		t.Errorf("expected 2 groups after PUT, got %d", len(h.cfg.ModelGroups))
	}
	if h.cfg.ModelGroups[0].Name != "new-a" {
		t.Errorf("expected first group 'new-a', got %q", h.cfg.ModelGroups[0].Name)
	}
}

func TestPutModelGroups_InvalidBody_Returns400(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	w := doRequest(t, h.PutModelGroups, http.MethodPut, "invalid-json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- PatchModelGroup ---

func TestPatchModelGroup_InsertsNewGroup(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	body := `{"value":{"name":"failover","models":[{"model":"m1","priority":1}]}}`
	w := doRequest(t, h.PatchModelGroup, http.MethodPatch, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.ModelGroups) != 1 || h.cfg.ModelGroups[0].Name != "failover" {
		t.Errorf("unexpected model groups after insert: %v", h.cfg.ModelGroups)
	}
}

func TestPatchModelGroup_UpdatesExistingGroup(t *testing.T) {
	cfg := &config.Config{
		ModelGroups: []config.ModelGroup{
			{Name: "failover", Models: []config.ModelGroupEntry{{Model: "old-model", Priority: 1}}},
		},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	body := `{"value":{"name":"failover","models":[{"model":"new-model","priority":2}]}}`
	w := doRequest(t, h.PatchModelGroup, http.MethodPatch, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.ModelGroups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(h.cfg.ModelGroups))
	}
	if len(h.cfg.ModelGroups[0].Models) != 1 || h.cfg.ModelGroups[0].Models[0].Model != "new-model" {
		t.Errorf("expected model updated to 'new-model', got %v", h.cfg.ModelGroups[0].Models)
	}
}

func TestPatchModelGroup_MissingName_Returns400(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	body := `{"value":{"models":[{"model":"m1","priority":1}]}}`
	w := doRequest(t, h.PatchModelGroup, http.MethodPatch, body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- DeleteModelGroup ---

func TestDeleteModelGroup_RemovesMatchingEntry(t *testing.T) {
	cfg := &config.Config{
		ModelGroups: []config.ModelGroup{
			{Name: "keep", Models: []config.ModelGroupEntry{{Model: "m1", Priority: 1}}},
			{Name: "remove", Models: []config.ModelGroupEntry{{Model: "m2", Priority: 1}}},
		},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	w := doRequestWithQuery(t, h.DeleteModelGroup, http.MethodDelete, "name=remove")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.ModelGroups) != 1 || h.cfg.ModelGroups[0].Name != "keep" {
		t.Errorf("unexpected groups after delete: %v", h.cfg.ModelGroups)
	}
}

func TestDeleteModelGroup_MissingQueryParam_Returns400(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	w := doRequestWithQuery(t, h.DeleteModelGroup, http.MethodDelete, "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteModelGroup_NonExistentName_NoOp(t *testing.T) {
	cfg := &config.Config{
		ModelGroups: []config.ModelGroup{
			{Name: "existing", Models: []config.ModelGroupEntry{{Model: "m1", Priority: 1}}},
		},
	}
	h, _ := newTestHandlerWithConfig(t, cfg)
	w := doRequestWithQuery(t, h.DeleteModelGroup, http.MethodDelete, "name=nonexistent")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(h.cfg.ModelGroups) != 1 {
		t.Errorf("expected 1 group after no-op delete, got %d", len(h.cfg.ModelGroups))
	}
}

// --- keyConfigRefreshFunc callback ---

func TestPutModelGroups_CallsRefreshFunc(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	called := false
	h.SetKeyConfigRefreshFunc(func() { called = true })
	body := `{"model-groups":[{"name":"g","models":[{"model":"m","priority":1}]}]}`
	doRequest(t, h.PutModelGroups, http.MethodPut, body)
	if !called {
		t.Error("expected refresh func to be called after PUT")
	}
}

func TestPatchModelGroup_CallsRefreshFunc(t *testing.T) {
	h, _ := newTestHandlerWithConfig(t, &config.Config{})
	called := false
	h.SetKeyConfigRefreshFunc(func() { called = true })
	body := `{"value":{"name":"g","models":[{"model":"m","priority":1}]}}`
	doRequest(t, h.PatchModelGroup, http.MethodPatch, body)
	if !called {
		t.Error("expected refresh func to be called after PATCH")
	}
}

func TestDeleteModelGroup_CallsRefreshFunc(t *testing.T) {
	cfg := &config.Config{ModelGroups: []config.ModelGroup{{Name: "g", Models: []config.ModelGroupEntry{{Model: "m", Priority: 1}}}}}
	h, _ := newTestHandlerWithConfig(t, cfg)
	called := false
	h.SetKeyConfigRefreshFunc(func() { called = true })
	doRequestWithQuery(t, h.DeleteModelGroup, http.MethodDelete, "name=g")
	if !called {
		t.Error("expected refresh func to be called after DELETE")
	}
}
