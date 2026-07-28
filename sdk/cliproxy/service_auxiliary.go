package cliproxy

import (
	"context"
	"errors"
	"time"

	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/warmup"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
)

type warmupAdapter struct{ service *Service }

func (a *warmupAdapter) TriggerNow(reason string) {
	if a == nil || a.service == nil {
		return
	}
	a.service.warmupMu.Lock()
	scheduler := a.service.warmupScheduler
	a.service.warmupMu.Unlock()
	if scheduler != nil {
		ctx := a.service.warmupCtx
		if ctx == nil {
			ctx = context.Background()
		}
		scheduler.TriggerNow(ctx, reason)
	}
}

func (*warmupAdapter) SupportedProviders() []string { return warmup.SupportedProviders() }

func (a *warmupAdapter) Reload() error {
	if a == nil || a.service == nil {
		return errors.New("cliproxy: service unavailable")
	}
	service := a.service
	service.cfgMu.RLock()
	cfg := service.cfg
	service.cfgMu.RUnlock()
	if cfg == nil {
		return errors.New("cliproxy: config unavailable")
	}
	options, err := warmup.ParseOptions(cfg.Warmup)
	if err != nil {
		return err
	}
	service.warmupMu.Lock()
	previous := service.warmupScheduler
	service.warmupScheduler = nil
	service.warmupMu.Unlock()
	if previous != nil {
		previous.Stop()
	}
	if options == nil {
		return nil
	}
	ctx := service.warmupCtx
	if ctx == nil {
		ctx = context.Background()
	}
	scheduler := warmup.NewScheduler(service.coreManager, *options)
	scheduler.Start(ctx)
	service.warmupMu.Lock()
	service.warmupScheduler = scheduler
	service.warmupMu.Unlock()
	return nil
}

func (s *Service) startUsagePersistor(ctx context.Context) {
	if s == nil || s.cfg == nil || s.cfg.UsagePersistence.Addr == "" {
		return
	}
	options := internalusage.PersistOptions{
		Addr: s.cfg.UsagePersistence.Addr, Password: s.cfg.UsagePersistence.Password,
		DB: s.cfg.UsagePersistence.DB, Key: s.cfg.UsagePersistence.Key,
	}
	if s.cfg.UsagePersistence.FlushIntervalSeconds > 0 {
		options.FlushInterval = time.Duration(s.cfg.UsagePersistence.FlushIntervalSeconds) * time.Second
	}
	persistor, err := internalusage.NewPersistor(options, internalusage.GetRequestStatistics())
	if err != nil {
		log.WithError(err).Warn("usage persistence disabled")
		return
	}
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := persistor.LoadSnapshot(loadCtx); err != nil {
		log.WithError(err).Warn("usage persistence snapshot load failed")
	}
	persistor.Start(ctx)
	s.usagePersistor = persistor
}

func (s *Service) startUsageAggregator(ctx context.Context) {
	if s == nil || s.cfg == nil || s.cfg.UsageAggregation.Addr == "" {
		return
	}
	options := internalusage.AggregatorOptions{
		Addr: s.cfg.UsageAggregation.Addr, Password: s.cfg.UsageAggregation.Password,
		DB: s.cfg.UsageAggregation.DB, Key: s.cfg.UsageAggregation.Key,
		RetentionDays: s.cfg.UsageAggregation.RetentionDays,
	}
	if s.cfg.UsageAggregation.IntervalSeconds > 0 {
		options.Interval = time.Duration(s.cfg.UsageAggregation.IntervalSeconds) * time.Second
	}
	aggregator, err := internalusage.NewAggregator(options, internalusage.GetRequestStatistics())
	if err != nil {
		log.WithError(err).Warn("usage aggregation disabled")
		return
	}
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := aggregator.Load(loadCtx); err != nil {
		log.WithError(err).Warn("usage aggregation state load failed")
	}
	aggregator.Start(ctx)
	internalusage.SetDefaultAggregator(aggregator)
	s.usageAggregator = aggregator
}

func (s *Service) reloadUsageServices(ctx context.Context, previous, current *config.Config) {
	if s == nil || current == nil {
		return
	}
	if previous == nil || previous.UsagePersistence != current.UsagePersistence {
		if s.usagePersistor != nil {
			s.usagePersistor.Stop()
			s.usagePersistor = nil
		}
		s.startUsagePersistor(ctx)
	}
	if previous == nil || previous.UsageAggregation != current.UsageAggregation {
		if s.usageAggregator != nil {
			s.usageAggregator.Stop()
			s.usageAggregator = nil
			internalusage.SetDefaultAggregator(nil)
		}
		s.startUsageAggregator(ctx)
	}
}
