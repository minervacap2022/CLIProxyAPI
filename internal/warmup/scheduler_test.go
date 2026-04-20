package warmup

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

// fakeExecutor records each provider-executor Execute call for assertions.
type fakeExecutor struct {
	mu    sync.Mutex
	auths []*coreauth.Auth

	calls atomic.Int64
	seen  []seenCall
	err   error
}

type seenCall struct {
	provider string
	authID   string
	model    string
}

func (f *fakeExecutor) List() []*coreauth.Auth {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*coreauth.Auth, len(f.auths))
	copy(out, f.auths)
	return out
}

func (f *fakeExecutor) Executor(provider string) (coreauth.ProviderExecutor, bool) {
	return &fakeProviderExecutor{parent: f, provider: provider}, true
}

// fakeProviderExecutor implements coreauth.ProviderExecutor minimally, recording
// the auth ID and model passed by the scheduler.
type fakeProviderExecutor struct {
	parent   *fakeExecutor
	provider string
}

func (p *fakeProviderExecutor) Identifier() string { return p.provider }

func (p *fakeProviderExecutor) Execute(_ context.Context, auth *coreauth.Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	p.parent.calls.Add(1)
	c := seenCall{provider: p.provider, model: req.Model}
	if auth != nil {
		c.authID = auth.ID
	}
	p.parent.mu.Lock()
	p.parent.seen = append(p.parent.seen, c)
	p.parent.mu.Unlock()
	return cliproxyexecutor.Response{}, p.parent.err
}

func (p *fakeProviderExecutor) ExecuteStream(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (p *fakeProviderExecutor) Refresh(context.Context, *coreauth.Auth) (*coreauth.Auth, error) {
	return nil, nil
}

func (p *fakeProviderExecutor) CountTokens(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (p *fakeProviderExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func oauthAuth(id, provider string) *coreauth.Auth {
	return &coreauth.Auth{
		ID:       id,
		Provider: provider,
		Metadata: map[string]any{"access_token": "oat-" + id},
	}
}

func TestParseOptions_DisabledReturnsNil(t *testing.T) {
	opts, err := ParseOptions(config.WarmupConfig{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts != nil {
		t.Fatalf("expected nil options when disabled, got %+v", opts)
	}
}

func TestParseOptions_RequiresAtLeastOneTrigger(t *testing.T) {
	_, err := ParseOptions(config.WarmupConfig{Enabled: true})
	if err == nil {
		t.Fatal("expected error when no trigger configured")
	}
}

func TestParseOptions_RejectsUnsupportedProvider(t *testing.T) {
	_, err := ParseOptions(config.WarmupConfig{
		Enabled:   true,
		OnStartup: true,
		Providers: []string{"does-not-exist"},
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestParseOptions_IntervalBelowMinRejected(t *testing.T) {
	_, err := ParseOptions(config.WarmupConfig{
		Enabled:  true,
		Interval: "1m",
	})
	if err == nil {
		t.Fatal("expected error for tiny interval")
	}
}

func TestParseOptions_ValidDefaults(t *testing.T) {
	opts, err := ParseOptions(config.WarmupConfig{
		Enabled:  true,
		Interval: "1h",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts == nil {
		t.Fatal("expected non-nil options")
	}
	if opts.Interval != time.Hour {
		t.Errorf("Interval=%s, want 1h", opts.Interval)
	}
	if opts.Timeout != defaultTimeout {
		t.Errorf("Timeout=%s, want default %s", opts.Timeout, defaultTimeout)
	}
	if opts.Concurrency != defaultConcurrency {
		t.Errorf("Concurrency=%d, want default %d", opts.Concurrency, defaultConcurrency)
	}
	if opts.Location == nil || opts.Location.String() != defaultTimezone {
		t.Errorf("Location=%v, want default %s", opts.Location, defaultTimezone)
	}
}

func TestParseOptions_CustomTimezone(t *testing.T) {
	opts, err := ParseOptions(config.WarmupConfig{
		Enabled:  true,
		Interval: "1h",
		Timezone: "America/New_York",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Location.String() != "America/New_York" {
		t.Errorf("Location=%s, want America/New_York", opts.Location)
	}
}

func TestParseOptions_InvalidTimezone(t *testing.T) {
	_, err := ParseOptions(config.WarmupConfig{
		Enabled:  true,
		Interval: "1h",
		Timezone: "Not/A/Real/Zone",
	})
	if err == nil {
		t.Fatal("expected error for bad timezone")
	}
}

func TestParseOptions_StartAtParsed(t *testing.T) {
	opts, err := ParseOptions(config.WarmupConfig{
		Enabled: true,
		StartAt: "08:45",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.StartAt == nil || opts.StartAt.hour != 8 || opts.StartAt.minute != 45 {
		t.Errorf("StartAt=%v, want 08:45", opts.StartAt)
	}
}

func TestParseOptions_ModelOverrides(t *testing.T) {
	opts, err := ParseOptions(config.WarmupConfig{
		Enabled:   true,
		OnStartup: true,
		Models: map[string]string{
			"claude": "claude-sonnet-4-6",
			"Gemini": "gemini-2.5-flash",
			"":       "ignored",
			"kimi":   "   ",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := opts.Models["claude"]; got != "claude-sonnet-4-6" {
		t.Errorf("claude override = %q, want claude-sonnet-4-6", got)
	}
	if got := opts.Models["gemini"]; got != "gemini-2.5-flash" {
		t.Errorf("gemini override (case normalised) = %q", got)
	}
	if _, ok := opts.Models["kimi"]; ok {
		t.Error("blank kimi override should be dropped")
	}
}

func TestParseOptions_ModelOverrideUnknownProvider(t *testing.T) {
	_, err := ParseOptions(config.WarmupConfig{
		Enabled:   true,
		OnStartup: true,
		Models:    map[string]string{"nope": "some-model"},
	})
	if err == nil {
		t.Fatal("expected error for unknown provider in models")
	}
}

func TestEligible(t *testing.T) {
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	s := &Scheduler{opts: Options{
		Providers: map[string]struct{}{},
		Location:  shanghai,
	}}

	tests := []struct {
		name string
		auth *coreauth.Auth
		want bool
	}{
		{
			name: "nil auth",
			auth: nil,
			want: false,
		},
		{
			name: "claude oauth (access_token)",
			auth: oauthAuth("claude-1", "claude"),
			want: true,
		},
		{
			name: "claude oauth (email only)",
			auth: &coreauth.Auth{Provider: "claude", Metadata: map[string]any{"email": "me@example.com"}},
			want: true,
		},
		{
			name: "claude api-key (excluded)",
			auth: &coreauth.Auth{Provider: "claude", Attributes: map[string]string{"api_key": "sk-ant-api03-xxx"}},
			want: false,
		},
		{
			name: "claude no credentials",
			auth: &coreauth.Auth{Provider: "claude"},
			want: false,
		},
		{
			name: "disabled auth",
			auth: func() *coreauth.Auth { a := oauthAuth("x", "claude"); a.Disabled = true; return a }(),
			want: false,
		},
		{
			name: "unavailable auth",
			auth: func() *coreauth.Auth { a := oauthAuth("x", "claude"); a.Unavailable = true; return a }(),
			want: false,
		},
		{
			name: "provider without recipe",
			auth: oauthAuth("oc-1", "openai-compat"),
			want: false,
		},
		{
			name: "empty provider",
			auth: &coreauth.Auth{Provider: "", Metadata: map[string]any{"access_token": "x"}},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := s.eligible(tc.auth)
			if got != tc.want {
				t.Errorf("eligible=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestEligible_ProviderAllowlist(t *testing.T) {
	s := &Scheduler{opts: Options{
		Providers: map[string]struct{}{"claude": {}},
		Location:  time.UTC,
	}}
	if !s.eligible(oauthAuth("c", "claude")) {
		t.Error("claude should be eligible in allowlist")
	}
	if s.eligible(oauthAuth("g", "gemini")) {
		t.Error("gemini should be excluded by allowlist")
	}
}

func TestRunRound_SkipsIneligibleAuths(t *testing.T) {
	fx := &fakeExecutor{
		auths: []*coreauth.Auth{
			oauthAuth("oauth-claude", "claude"),
			{ID: "apikey-claude", Provider: "claude", Attributes: map[string]string{"api_key": "sk-xxx"}},
			func() *coreauth.Auth {
				a := oauthAuth("disabled-codex", "codex")
				a.Disabled = true
				return a
			}(),
			oauthAuth("oauth-gemini", "gemini"),
			{ID: "no-creds-claude", Provider: "claude"},
		},
	}
	s := NewScheduler(fx, Options{Concurrency: 1, Timeout: time.Second, Location: time.UTC})
	s.runRound(context.Background(), "test")

	// Only oauth-claude and oauth-gemini are eligible (2 calls).
	if got := fx.calls.Load(); got != 2 {
		t.Errorf("Execute call count = %d, want 2", got)
	}
}

func TestRunRound_ContinuesAfterError(t *testing.T) {
	fx := &fakeExecutor{
		auths: []*coreauth.Auth{
			oauthAuth("a1", "claude"),
			oauthAuth("a2", "claude"),
		},
		err: context.DeadlineExceeded,
	}
	s := NewScheduler(fx, Options{Concurrency: 1, Timeout: time.Second, Location: time.UTC})
	s.runRound(context.Background(), "test")
	if got := fx.calls.Load(); got != 2 {
		t.Errorf("Execute call count = %d, want 2 (errors should not short-circuit)", got)
	}
}

func TestRunRound_UsesPinnedAuthMetadata(t *testing.T) {
	fx := &fakeExecutor{auths: []*coreauth.Auth{oauthAuth("pin-me", "claude")}}
	s := NewScheduler(fx, Options{Concurrency: 1, Timeout: time.Second, Location: time.UTC})
	s.runRound(context.Background(), "test")

	fx.mu.Lock()
	defer fx.mu.Unlock()
	foundPin := false
	for _, c := range fx.seen {
		if c.authID == "pin-me" {
			foundPin = true
		}
	}
	if !foundPin {
		t.Errorf("expected pinned auth id 'pin-me' in Execute metadata, got seen=%+v", fx.seen)
	}
}

func TestRunRound_RespectsCtxCancelMidRun(t *testing.T) {
	// Many auths but concurrency=1 so the producer will block on sem.
	auths := make([]*coreauth.Auth, 10)
	for i := range auths {
		auths[i] = oauthAuth("claude-"+string(rune('a'+i)), "claude")
	}
	fx := &fakeExecutor{auths: auths}

	// Slow executor so rounds don't complete instantly.
	fx.err = errors.New("slow")

	s := NewScheduler(fx, Options{Concurrency: 1, Timeout: 5 * time.Second, Location: time.UTC})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.runRound(ctx, "cancel-test")
		close(done)
	}()
	time.AfterFunc(50*time.Millisecond, cancel)

	select {
	case <-done:
		// Round returned — must have observed the cancellation.
	case <-time.After(3 * time.Second):
		t.Fatal("runRound did not return after context cancellation (goroutine leak risk)")
	}
}

func TestRunRound_ModelOverride(t *testing.T) {
	fx := &fakeExecutor{auths: []*coreauth.Auth{oauthAuth("c", "claude")}}
	s := NewScheduler(fx, Options{
		Concurrency: 1,
		Timeout:     time.Second,
		Models:      map[string]string{"claude": "claude-sonnet-4-6"},
		Location:    time.UTC,
	})
	s.runRound(context.Background(), "test")

	fx.mu.Lock()
	defer fx.mu.Unlock()
	if len(fx.seen) != 1 || fx.seen[0].model != "claude-sonnet-4-6" {
		t.Errorf("expected model override, seen=%+v", fx.seen)
	}
}
