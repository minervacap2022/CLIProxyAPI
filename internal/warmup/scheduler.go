package warmup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

const (
	defaultConcurrency = 2
	defaultTimeout     = 30 * time.Second
	minWarmupInterval  = 15 * time.Minute // sanity floor; avoid accidental flood
	defaultTimezone    = "Asia/Shanghai"  // UTC+8 — primary operator locale
)

// Options are resolved settings derived from config.WarmupConfig.
// A nil *Options means warmup is disabled.
type Options struct {
	Interval    time.Duration
	StartAt     *timeOfDay          // nil when unset
	Location    *time.Location      // non-nil; zone StartAt is interpreted in
	OnStartup   bool
	Providers   map[string]struct{} // empty map = allow all supported
	Models      map[string]string   // provider -> override model
	Concurrency int
	Timeout     time.Duration
}

// ParseOptions validates a WarmupConfig and returns resolved Options.
// Returns (nil, nil) when the config disables warmup.
// Returns an error for invalid values (unparseable interval, bad HH:MM, etc.).
func ParseOptions(cfg config.WarmupConfig) (*Options, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	opts := &Options{
		OnStartup:   cfg.OnStartup,
		Concurrency: cfg.Concurrency,
		Providers:   make(map[string]struct{}, len(cfg.Providers)),
		Models:      make(map[string]string, len(cfg.Models)),
	}

	zoneName := strings.TrimSpace(cfg.Timezone)
	if zoneName == "" {
		zoneName = defaultTimezone
	}
	loc, err := time.LoadLocation(zoneName)
	if err != nil {
		return nil, fmt.Errorf("warmup.timezone %q: %w", zoneName, err)
	}
	opts.Location = loc

	if s := strings.TrimSpace(cfg.Interval); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("warmup.interval: %w", err)
		}
		if d > 0 && d < minWarmupInterval {
			return nil, fmt.Errorf("warmup.interval: %s is below the %s minimum", d, minWarmupInterval)
		}
		opts.Interval = d
	}

	if s := strings.TrimSpace(cfg.StartAt); s != "" {
		tod, err := parseTimeOfDay(s)
		if err != nil {
			return nil, fmt.Errorf("warmup.start-at: %w", err)
		}
		opts.StartAt = &tod
	}

	if s := strings.TrimSpace(cfg.Timeout); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("warmup.timeout: %w", err)
		}
		if d > 0 {
			opts.Timeout = d
		}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = defaultConcurrency
	}

	for _, p := range cfg.Providers {
		key := strings.ToLower(strings.TrimSpace(p))
		if key == "" {
			continue
		}
		if _, ok := recipes[key]; !ok {
			return nil, fmt.Errorf("warmup.providers: unsupported provider %q (supported: %v)", p, SupportedProviders())
		}
		opts.Providers[key] = struct{}{}
	}

	for provider, model := range cfg.Models {
		key := strings.ToLower(strings.TrimSpace(provider))
		if key == "" {
			continue
		}
		if _, ok := recipes[key]; !ok {
			return nil, fmt.Errorf("warmup.models: unsupported provider %q (supported: %v)", provider, SupportedProviders())
		}
		trimmed := strings.TrimSpace(model)
		if trimmed == "" {
			continue
		}
		opts.Models[key] = trimmed
	}

	// Enforce at least one trigger when enabled; otherwise the scheduler is a no-op.
	if opts.Interval <= 0 && opts.StartAt == nil && !opts.OnStartup {
		return nil, fmt.Errorf("warmup: must set interval, start-at, or on-startup when enabled")
	}

	return opts, nil
}

// Executor is the subset of *coreauth.Manager the scheduler depends on.
// Defining it locally keeps the scheduler testable without the full manager.
//
// We intentionally call the provider executor directly (rather than going
// through Manager.Execute) so warmup bypasses the model-registry filter.
// Warmup requests target a specific OAuth auth + a known cheap model; the
// Manager's "does this auth advertise support for this model" gate does not
// reflect upstream API availability and was rejecting legitimate warmup
// traffic when operators pinned their auths to a custom model list.
type Executor interface {
	List() []*coreauth.Auth
	Executor(provider string) (coreauth.ProviderExecutor, bool)
}

// Scheduler fires warmup requests on interval + start-time triggers.
// Zero-value Scheduler is not usable; call NewScheduler.
type Scheduler struct {
	mgr  Executor
	opts Options
	now  func() time.Time // injectable clock for tests

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup // tracks active goroutines
}

// NewScheduler builds a scheduler. The mgr argument is the auth manager that
// owns the OAuth auths; opts must come from ParseOptions.
func NewScheduler(mgr Executor, opts Options) *Scheduler {
	if opts.Location == nil {
		opts.Location = time.Local
	}
	return &Scheduler{
		mgr:  mgr,
		opts: opts,
		now:  time.Now,
	}
}

// Start launches the scheduler goroutines. Calling Start twice is safe; the
// second call stops the previous run and waits for it to drain before
// launching new goroutines.
func (s *Scheduler) Start(parent context.Context) {
	if s == nil || s.mgr == nil {
		return
	}
	// Stop any previous run and wait for it to finish before launching new
	// goroutines; this prevents two rounds racing on startup.
	s.Stop()

	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	nextDaily := ""
	if s.opts.StartAt != nil {
		nextDaily = s.opts.StartAt.nextFrom(s.now().In(s.opts.Location)).Format(time.RFC3339)
	}
	log.WithFields(log.Fields{
		"interval":    s.opts.Interval.String(),
		"start_at":    startAtString(s.opts.StartAt),
		"timezone":    s.opts.Location.String(),
		"next_daily":  nextDaily,
		"on_startup":  s.opts.OnStartup,
		"concurrency": s.opts.Concurrency,
		"timeout":     s.opts.Timeout.String(),
		"providers":   providerAllowlist(s.opts.Providers),
	}).Info("warmup scheduler started")

	if s.opts.OnStartup {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runRound(ctx, "startup")
		}()
	}
	if s.opts.Interval > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.intervalLoop(ctx)
		}()
	}
	if s.opts.StartAt != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.dailyLoop(ctx)
		}()
	}
}

// TriggerNow fires a single warmup round synchronously with the given reason.
// Useful for management-API "warm now" endpoints and integration tests.
// Reason defaults to "manual" when empty.
func (s *Scheduler) TriggerNow(ctx context.Context, reason string) {
	if s == nil || s.mgr == nil {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "manual"
	}
	s.runRound(ctx, reason)
}

// Stop cancels the scheduler and waits for all in-flight goroutines to exit.
// Safe to call multiple times and on a nil scheduler.
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *Scheduler) intervalLoop(ctx context.Context) {
	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRound(ctx, "interval")
		}
	}
}

func (s *Scheduler) dailyLoop(ctx context.Context) {
	for {
		ref := s.now().In(s.opts.Location)
		next := s.opts.StartAt.nextFrom(ref)
		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		log.WithFields(log.Fields{
			"next_fire_at":     next.In(s.opts.Location).Format(time.RFC3339),
			"next_fire_at_utc": next.UTC().Format(time.RFC3339),
			"wait":             wait.String(),
			"timezone":         s.opts.Location.String(),
		}).Debug("warmup daily loop sleeping until next fire time")
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.runRound(ctx, "scheduled")
		}
	}
}

// runRound walks the auth list once, fans work out to Concurrency workers.
// Never returns an error — per-auth failures are logged and skipped.
// Honours ctx.Done for clean shutdown mid-round.
func (s *Scheduler) runRound(ctx context.Context, trigger string) {
	roundID := uuid.NewString()[:8]
	roundLogger := log.WithFields(log.Fields{
		"round_id":  roundID,
		"trigger":   trigger,
		"timezone":  s.opts.Location.String(),
		"round_utc": time.Now().UTC().Format(time.RFC3339),
	})

	auths := s.eligibleAuths()
	if len(auths) == 0 {
		roundLogger.Info("warmup round skipped: no eligible OAuth auths")
		return
	}
	roundLogger.WithField("auth_count", len(auths)).Info("warmup round started")

	sem := make(chan struct{}, s.opts.Concurrency)
	var wg sync.WaitGroup
	var ok, fail atomic.Int64
	roundStart := time.Now()

producerLoop:
	for i := range auths {
		auth := auths[i]
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break producerLoop
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if s.warmOne(ctx, auth, trigger, roundID) {
				ok.Add(1)
			} else {
				fail.Add(1)
			}
		}()
	}
	wg.Wait()

	summary := log.Fields{
		"ok":       ok.Load(),
		"fail":     fail.Load(),
		"total":    len(auths),
		"duration": time.Since(roundStart).String(),
	}
	if ctx.Err() != nil {
		roundLogger.WithFields(summary).Warn("warmup round aborted: context cancelled")
		return
	}
	roundLogger.WithFields(summary).Info("warmup round finished")
}

// warmOne fires a single warmup request for the given auth.
// Returns true on success, false on any failure.
func (s *Scheduler) warmOne(ctx context.Context, auth *coreauth.Auth, trigger, roundID string) bool {
	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	recipe, ok := lookupRecipe(provider)
	if !ok {
		log.WithFields(log.Fields{
			"round_id": roundID,
			"auth_id":  auth.ID,
			"provider": provider,
		}).Debug("warmup skipped: no recipe for provider")
		return false
	}

	// Per-provider model override lets operators swap to a cheaper/faster model.
	model := recipe.Model
	payload := recipe.Payload
	if override, has := s.opts.Models[provider]; has && override != "" {
		model = override
		payload = overrideModelInPayload(recipe, override)
	}

	entry := log.WithFields(log.Fields{
		"round_id":    roundID,
		"trigger":     trigger,
		"auth_id":     auth.ID,
		"auth_label":  auth.Label,
		"provider":    provider,
		"model":       model,
		"timezone":    s.opts.Location.String(),
		"started_utc": time.Now().UTC().Format(time.RFC3339),
	})

	providerExec, hasExec := s.mgr.Executor(provider)
	if !hasExec || providerExec == nil {
		entry.Warn("warmup skipped: provider executor not registered")
		return false
	}

	reqCtx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()

	req := cliproxyexecutor.Request{
		Model:   model,
		Payload: payload,
		Format:  recipe.SourceFormat,
		Metadata: map[string]any{
			cliproxyexecutor.PinnedAuthMetadataKey: auth.ID,
			"warmup":                               true,
		},
	}
	execOpts := cliproxyexecutor.Options{
		SourceFormat:    recipe.SourceFormat,
		OriginalRequest: payload,
		Metadata: map[string]any{
			cliproxyexecutor.PinnedAuthMetadataKey: auth.ID,
			"warmup":                               true,
		},
	}

	start := s.now()
	_, err := providerExec.Execute(reqCtx, auth, req, execOpts)
	dur := time.Since(start)
	if err != nil {
		fields := log.Fields{"duration": dur.String(), "error": err.Error()}
		var se cliproxyexecutor.StatusError
		if errors.As(err, &se) {
			fields["http_status"] = se.StatusCode()
		}
		entry.WithFields(fields).Warn("warmup failed")
		return false
	}
	entry.WithField("duration", dur.String()).Info("warmup ok")
	return true
}

// eligibleAuths returns OAuth auths that have a recipe and are not disabled.
// API-key auths are excluded because they have no session window to warm.
func (s *Scheduler) eligibleAuths() []*coreauth.Auth {
	all := s.mgr.List()
	out := make([]*coreauth.Auth, 0, len(all))
	for _, a := range all {
		if !s.eligible(a) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// eligible applies per-auth filters. Extracted for unit testing.
//
// An auth is eligible only when it is OAuth-backed (has access_token or an
// OAuth email in Metadata) AND does not carry an Attributes["api_key"].
// API-key auths have no session window to warm, so they are skipped.
func (s *Scheduler) eligible(a *coreauth.Auth) bool {
	if a == nil || a.Disabled || a.Unavailable {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(a.Provider))
	if provider == "" {
		return false
	}
	if _, ok := recipes[provider]; !ok {
		return false
	}
	if len(s.opts.Providers) > 0 {
		if _, allowed := s.opts.Providers[provider]; !allowed {
			return false
		}
	}
	// API-key auths are not subject to session windows — skip them.
	if a.Attributes != nil && strings.TrimSpace(a.Attributes["api_key"]) != "" {
		return false
	}
	// Positive OAuth signal: metadata carries an access_token or a login email.
	// This avoids pinging half-initialised auths that have neither credential.
	if !hasOAuthCredential(a) {
		return false
	}
	return true
}

// hasOAuthCredential returns true when an auth carries an OAuth access_token
// or a login email address in its metadata.
func hasOAuthCredential(a *coreauth.Auth) bool {
	if a == nil || a.Metadata == nil {
		return false
	}
	if v, ok := a.Metadata["access_token"].(string); ok && strings.TrimSpace(v) != "" {
		return true
	}
	if v, ok := a.Metadata["email"].(string); ok && strings.TrimSpace(v) != "" {
		return true
	}
	return false
}

// startAtString renders a nullable time-of-day for logging.
func startAtString(t *timeOfDay) string {
	if t == nil {
		return ""
	}
	return t.String()
}

// providerAllowlist returns the sorted allowlist keys for logging.
func providerAllowlist(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
