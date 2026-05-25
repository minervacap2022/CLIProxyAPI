// Package usage — persistence layer.
//
// Wraps the in-memory RequestStatistics with a Redis-backed snapshot:
//   - on startup: try to load the previous snapshot from Redis into memory;
//   - while running: every flushInterval the snapshot is serialized and
//     persisted (when there is unsaved progress);
//   - on shutdown: one final flush.
//
// If Redis is unreachable NewPersistor returns an error and the caller is
// expected to log it and continue in pure in-memory mode (no flush, no load).
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
)

const (
	defaultRedisKey       = "cpa:usage:snapshot"
	defaultFlushInterval  = 5 * time.Second
	defaultPersistTimeout = 3 * time.Second
)

// PersistOptions configures the Redis-backed snapshot persistor.
type PersistOptions struct {
	Addr     string // "host:port"
	Password string
	DB       int
	Key      string // Redis key for the snapshot (default: cpa:usage:snapshot)

	FlushInterval time.Duration // default 5s
}

// Persistor writes usage snapshots to Redis on a schedule.
type Persistor struct {
	stats  *RequestStatistics
	client *redis.Client
	opts   PersistOptions

	// lastRequestCount is the TotalRequests value at the last successful
	// flush; we only re-serialize+write when it diverges from the current
	// snapshot, avoiding write amplification on idle traffic.
	lastRequestCount atomic.Int64

	stopCh  chan struct{}
	doneCh  chan struct{}
	once    sync.Once
	started atomic.Bool
}

// NewPersistor pings Redis and returns a ready Persistor. Returning an
// error means we could NOT establish a connection.
func NewPersistor(opts PersistOptions, stats *RequestStatistics) (*Persistor, error) {
	if stats == nil {
		return nil, errors.New("usage: stats is nil")
	}
	if opts.Addr == "" {
		return nil, errors.New("usage: redis addr is empty")
	}
	if opts.Key == "" {
		opts.Key = defaultRedisKey
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = defaultFlushInterval
	}

	cli := redis.NewClient(&redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultPersistTimeout)
	defer cancel()
	if err := cli.Ping(ctx).Err(); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("usage: redis ping failed: %w", err)
	}

	return &Persistor{
		stats:  stats,
		client: cli,
		opts:   opts,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}, nil
}

// LoadSnapshot pulls the last snapshot from Redis and merges it into the
// in-memory stats. No-op (nil error) if no prior snapshot exists.
func (p *Persistor) LoadSnapshot(ctx context.Context) error {
	if p == nil || p.client == nil {
		return nil
	}
	raw, err := p.client.Get(ctx, p.opts.Key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			log.Info("usage: no prior snapshot in redis (cold start)")
			return nil
		}
		return fmt.Errorf("usage: redis get failed: %w", err)
	}
	var snap StatisticsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return fmt.Errorf("usage: snapshot deserialize failed: %w", err)
	}
	merged := p.stats.MergeSnapshot(snap)
	p.lastRequestCount.Store(p.stats.Snapshot().TotalRequests)
	log.WithFields(log.Fields{
		"added":          merged.Added,
		"skipped":        merged.Skipped,
		"total_requests": p.lastRequestCount.Load(),
	}).Info("usage: snapshot loaded from redis")
	return nil
}

// Start begins the background flush loop. Returns immediately.
// Stop() must be called to flush+close cleanly. Calling Start more than
// once is a no-op.
func (p *Persistor) Start(ctx context.Context) {
	if p == nil {
		return
	}
	if !p.started.CompareAndSwap(false, true) {
		return
	}
	go p.loop(ctx)
}

// loop is the flush goroutine.
func (p *Persistor) loop(ctx context.Context) {
	defer close(p.doneCh)

	ticker := time.NewTicker(p.opts.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.flushOnce(context.Background())
			return
		case <-p.stopCh:
			p.flushOnce(context.Background())
			return
		case <-ticker.C:
			p.flushOnce(ctx)
		}
	}
}

func (p *Persistor) flushOnce(ctx context.Context) {
	snap := p.stats.Snapshot()
	if snap.TotalRequests == p.lastRequestCount.Load() {
		// nothing changed since last flush
		return
	}
	data, err := json.Marshal(snap)
	if err != nil {
		log.WithError(err).Warn("usage: snapshot marshal failed")
		return
	}

	flushCtx, cancel := context.WithTimeout(ctx, defaultPersistTimeout)
	defer cancel()
	if err := p.client.Set(flushCtx, p.opts.Key, data, 0).Err(); err != nil {
		log.WithError(err).Warn("usage: redis set failed; will retry on next tick")
		return
	}
	p.lastRequestCount.Store(snap.TotalRequests)
}

// Stop signals the flush loop to do one last flush and exit.
// Safe to call multiple times. Safe to call without a prior Start (the
// flush loop just isn't running, so we only need to close the client).
func (p *Persistor) Stop() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.started.Load() {
			close(p.stopCh)
			<-p.doneCh
		}
		_ = p.client.Close()
	})
}
