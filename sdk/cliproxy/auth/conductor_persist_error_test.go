package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	loghook "github.com/sirupsen/logrus/hooks/test"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// failingStore always returns an error from Save.
type failingStore struct {
	err error
}

func (s *failingStore) List(context.Context) ([]*Auth, error) { return nil, nil }
func (s *failingStore) Save(context.Context, *Auth) (string, error) {
	return "", s.err
}
func (s *failingStore) Delete(context.Context, string) error { return nil }

// TestPersist_StoreErrorIsLogged verifies that a Save failure in persist() emits a Warn log
// and does not silently swallow the error.
func TestPersist_StoreErrorIsLogged(t *testing.T) {
	saveErr := errors.New("disk full")
	store := &failingStore{err: saveErr}

	hook := loghook.NewLocal(log.StandardLogger())
	t.Cleanup(func() {
		log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	})

	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-persist-err",
		Provider: "claude",
		Metadata: map[string]any{"type": "claude"},
	}

	// Register triggers persist; the store will fail.
	if _, err := mgr.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}

	// Expect at least one Warn entry containing "auth persist failed".
	found := false
	for _, entry := range hook.Entries {
		if entry.Level == log.WarnLevel && entry.Message == "auth persist failed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'auth persist failed' Warn log; got entries: %v", hook.Entries)
	}
}

// refreshSuccessExecutor returns the auth unchanged (simulates a successful refresh).
type refreshSuccessExecutor struct{}

func (refreshSuccessExecutor) Identifier() string { return "claude" }
func (refreshSuccessExecutor) Execute(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (refreshSuccessExecutor) ExecuteStream(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}
func (refreshSuccessExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	updated := auth.Clone()
	updated.Metadata["access_token"] = "new-token"
	exp := time.Now().Add(8 * time.Hour).Format(time.RFC3339)
	updated.Metadata["expired"] = exp
	return updated, nil
}
func (refreshSuccessExecutor) CountTokens(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (refreshSuccessExecutor) HttpRequest(_ context.Context, _ *Auth, _ *http.Request) (*http.Response, error) {
	return nil, nil
}

// TestRefreshAuth_PersistFailureIsLogged verifies that when refreshAuth succeeds but
// the subsequent Update/persist fails, a Warn log is emitted.
func TestRefreshAuth_PersistFailureIsLogged(t *testing.T) {
	saveErr := errors.New("write error")
	store := &failingStore{err: saveErr}

	hook := loghook.NewLocal(log.StandardLogger())
	t.Cleanup(func() {
		log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	})

	// Build manager with a failing store and a successful executor.
	mgr := NewManager(store, nil, nil)
	mgr.RegisterExecutor(refreshSuccessExecutor{})

	auth := &Auth{
		ID:       "auth-refresh-persist",
		Provider: "claude",
		Metadata: map[string]any{
			"type":          "oauth",
			"refresh_token": "rt-abc",
			"access_token":  "at-old",
			"expired":       time.Now().Add(-time.Minute).Format(time.RFC3339),
		},
	}
	// Pre-populate the manager without going through store (store always fails).
	mgr.mu.Lock()
	mgr.auths[auth.ID] = auth.Clone()
	mgr.mu.Unlock()

	// Clear hook entries accumulated so far (from any internal init calls).
	hook.Reset()

	// Invoke refreshAuth directly — refresh will succeed, persist will fail.
	mgr.refreshAuth(context.Background(), auth.ID)

	// Expect at least one Warn entry: either "auth persist failed" or "auth refresh update failed".
	found := false
	for _, entry := range hook.Entries {
		if entry.Level == log.WarnLevel &&
			(entry.Message == "auth persist failed" || entry.Message == "auth refresh update failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected persist-failure Warn log; got entries: %+v", hook.Entries)
	}
}
