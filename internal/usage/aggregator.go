// Package usage — hourly pre-aggregation.
//
// The legacy /usage snapshot keeps every per-request detail forever, so any
// api_key × model summary has to walk the entire detail history on each read.
// The Aggregator folds those details into hourly buckets keyed by
// (api_key, model, hour) on its own ticker, keeps them in memory for O(buckets)
// reads, and mirrors them to Redis so they survive restarts.
//
// Incremental folding relies on the fact that detail slices are append-only
// (details are never trimmed): a per-(api_key, model) processed index is a
// stable cursor that never reprocesses or skips a record.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
)

const (
	defaultAggInterval        = 5 * time.Minute
	defaultAggKey             = "cpa:usage:agg"
	defaultAggCursorKey       = "cpa:usage:agg:cursor"
	aggFieldSep               = "\x1f"
	aggOpTimeout              = 3 * time.Second
	secondsPerHour      int64 = 3600
)

// defaultAggregator holds the process-wide aggregator so HTTP handlers can read
// pre-aggregated summaries without threading it through every call site.
var defaultAggregator atomic.Pointer[Aggregator]

// SetDefaultAggregator registers (or clears with nil) the process-wide
// aggregator used by DefaultAggregator.
func SetDefaultAggregator(a *Aggregator) { defaultAggregator.Store(a) }

// DefaultAggregator returns the process-wide aggregator, or nil when none is
// configured.
func DefaultAggregator() *Aggregator { return defaultAggregator.Load() }

// aggBucket accumulates counters for a single (api_key, model, hour) cell.
type aggBucket struct {
	Requests        int64 `json:"r"`
	Failed          int64 `json:"f"`
	InputTokens     int64 `json:"i"`
	OutputTokens    int64 `json:"o"`
	ReasoningTokens int64 `json:"re"`
	CachedTokens    int64 `json:"c"`
	TotalTokens     int64 `json:"t"`
}

// bucketKey identifies a bucket by api key, model, and hour epoch (unix/3600).
type bucketKey struct {
	api   string
	model string
	hour  int64
}

func (k bucketKey) field() string {
	return k.api + aggFieldSep + k.model + aggFieldSep + strconv.FormatInt(k.hour, 10)
}

func parseBucketField(field string) (bucketKey, bool) {
	parts := strings.Split(field, aggFieldSep)
	if len(parts) != 3 {
		return bucketKey{}, false
	}
	hour, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return bucketKey{}, false
	}
	return bucketKey{api: parts[0], model: parts[1], hour: hour}, true
}

// AggregatorOptions configures the hourly aggregator.
type AggregatorOptions struct {
	Addr     string
	Password string
	DB       int
	Key      string // Redis hash for buckets (default cpa:usage:agg)
	Interval time.Duration
	// RetentionDays prunes buckets older than N days on each tick. Zero keeps
	// all buckets.
	RetentionDays int
}

// Aggregator folds usage details into hourly buckets on its own ticker.
type Aggregator struct {
	stats     *RequestStatistics
	client    *redis.Client
	bucketKey string
	cursorKey string
	opts      AggregatorOptions

	mu      sync.RWMutex
	buckets map[bucketKey]*aggBucket
	cursor  map[string]int // "api\x1fmodel" -> processed detail count

	stopCh  chan struct{}
	doneCh  chan struct{}
	once    sync.Once
	started atomic.Bool
}

// NewAggregator pings Redis and returns a ready Aggregator.
func NewAggregator(opts AggregatorOptions, stats *RequestStatistics) (*Aggregator, error) {
	if stats == nil {
		return nil, errors.New("usage: stats is nil")
	}
	if opts.Addr == "" {
		return nil, errors.New("usage: redis addr is empty")
	}
	if opts.Key == "" {
		opts.Key = defaultAggKey
	}
	if opts.Interval <= 0 {
		opts.Interval = defaultAggInterval
	}

	cli := redis.NewClient(&redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), aggOpTimeout)
	defer cancel()
	if err := cli.Ping(ctx).Err(); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("usage: redis ping failed: %w", err)
	}

	return &Aggregator{
		stats:     stats,
		client:    cli,
		bucketKey: opts.Key,
		cursorKey: opts.Key + ":cursor",
		opts:      opts,
		buckets:   make(map[bucketKey]*aggBucket),
		cursor:    make(map[string]int),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}, nil
}

// Load restores buckets and the cursor from Redis. No-op (nil error) when no
// prior state exists.
func (a *Aggregator) Load(ctx context.Context) error {
	if a == nil || a.client == nil {
		return nil
	}
	fields, err := a.client.HGetAll(ctx, a.bucketKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("usage: agg hgetall failed: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for field, raw := range fields {
		key, ok := parseBucketField(field)
		if !ok {
			continue
		}
		var b aggBucket
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			continue
		}
		bucket := b
		a.buckets[key] = &bucket
	}
	raw, err := a.client.Get(ctx, a.cursorKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil
		}
		return fmt.Errorf("usage: agg cursor get failed: %w", err)
	}
	if err := json.Unmarshal(raw, &a.cursor); err != nil {
		a.cursor = make(map[string]int)
	}
	log.WithFields(log.Fields{"buckets": len(a.buckets), "cursors": len(a.cursor)}).Info("usage: aggregator state loaded from redis")
	return nil
}

// Start begins the background aggregation loop. Calling it more than once is a
// no-op.
func (a *Aggregator) Start(ctx context.Context) {
	if a == nil {
		return
	}
	if !a.started.CompareAndSwap(false, true) {
		return
	}
	go a.loop(ctx)
}

func (a *Aggregator) loop(ctx context.Context) {
	defer close(a.doneCh)
	ticker := time.NewTicker(a.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			a.tick(context.Background())
			return
		case <-a.stopCh:
			a.tick(context.Background())
			return
		case <-ticker.C:
			a.tick(ctx)
		}
	}
}

// tick folds new details into buckets and flushes dirty buckets to Redis.
func (a *Aggregator) tick(ctx context.Context) {
	dirty := a.fold()
	if a.opts.RetentionDays > 0 {
		a.prune()
	}
	a.flush(ctx, dirty)
}

// fold walks un-processed details for each (api, model) and accumulates them
// into hourly buckets. Returns the set of buckets that changed.
func (a *Aggregator) fold() map[bucketKey]struct{} {
	dirty := make(map[bucketKey]struct{})

	a.stats.mu.RLock()
	type pending struct {
		key    bucketKey
		bucket aggBucket
	}
	var updates []pending
	cursorUpdates := make(map[string]int)
	for apiName, stats := range a.stats.apis {
		if stats == nil {
			continue
		}
		for modelName, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			cursorKey := apiName + aggFieldSep + modelName
			// processed is the absolute number of details already folded for this
			// (api, model). base is how many were dropped from the front of the
			// retained slice, so absolute index i maps to Details[i-base].
			processed := int64(a.cursor[cursorKey])
			details := modelStatsValue.Details
			base := modelStatsValue.DetailsBase
			total := base + int64(len(details))
			if processed >= total {
				continue
			}
			// If the cursor fell behind the retained window (details were dropped
			// before being folded), resume from the oldest retained entry.
			start := processed
			if start < base {
				start = base
			}
			perHour := make(map[int64]*aggBucket)
			for i := start; i < total; i++ {
				d := &details[i-base]
				hour := d.Timestamp.Unix() / secondsPerHour
				b := perHour[hour]
				if b == nil {
					b = &aggBucket{}
					perHour[hour] = b
				}
				b.Requests++
				if d.Failed {
					b.Failed++
				}
				b.InputTokens += d.Tokens.InputTokens
				b.OutputTokens += d.Tokens.OutputTokens
				b.ReasoningTokens += d.Tokens.ReasoningTokens
				b.CachedTokens += d.Tokens.CachedTokens
				b.TotalTokens += d.Tokens.TotalTokens
			}
			for hour, b := range perHour {
				updates = append(updates, pending{key: bucketKey{api: apiName, model: modelName, hour: hour}, bucket: *b})
			}
			cursorUpdates[cursorKey] = int(total)
		}
	}
	a.stats.mu.RUnlock()

	if len(updates) == 0 {
		return dirty
	}

	a.mu.Lock()
	for _, u := range updates {
		existing := a.buckets[u.key]
		if existing == nil {
			existing = &aggBucket{}
			a.buckets[u.key] = existing
		}
		existing.Requests += u.bucket.Requests
		existing.Failed += u.bucket.Failed
		existing.InputTokens += u.bucket.InputTokens
		existing.OutputTokens += u.bucket.OutputTokens
		existing.ReasoningTokens += u.bucket.ReasoningTokens
		existing.CachedTokens += u.bucket.CachedTokens
		existing.TotalTokens += u.bucket.TotalTokens
		dirty[u.key] = struct{}{}
	}
	for k, v := range cursorUpdates {
		a.cursor[k] = v
	}
	a.mu.Unlock()
	return dirty
}

// prune drops buckets older than RetentionDays.
func (a *Aggregator) prune() {
	cutoff := time.Now().Add(-time.Duration(a.opts.RetentionDays)*24*time.Hour).Unix() / secondsPerHour
	a.mu.Lock()
	defer a.mu.Unlock()
	for k := range a.buckets {
		if k.hour < cutoff {
			delete(a.buckets, k)
		}
	}
}

// flush writes dirty buckets and the cursor to Redis.
func (a *Aggregator) flush(ctx context.Context, dirty map[bucketKey]struct{}) {
	if a == nil || a.client == nil || len(dirty) == 0 {
		return
	}
	a.mu.RLock()
	values := make(map[string]interface{}, len(dirty))
	for k := range dirty {
		b := a.buckets[k]
		if b == nil {
			continue
		}
		raw, err := json.Marshal(b)
		if err != nil {
			continue
		}
		values[k.field()] = raw
	}
	cursorRaw, _ := json.Marshal(a.cursor)
	a.mu.RUnlock()

	if len(values) == 0 {
		return
	}
	flushCtx, cancel := context.WithTimeout(ctx, aggOpTimeout)
	defer cancel()
	pipe := a.client.Pipeline()
	pipe.HSet(flushCtx, a.bucketKey, values)
	pipe.Set(flushCtx, a.cursorKey, cursorRaw, 0)
	if _, err := pipe.Exec(flushCtx); err != nil {
		log.WithError(err).Warn("usage: aggregator flush failed; will retry on next tick")
	}
}

// Summary returns api_key × model rows summed over buckets whose hour overlaps
// [start, end]. Zero-value bounds are unbounded.
func (a *Aggregator) Summary(start, end time.Time) []SummaryRow {
	if a == nil {
		return nil
	}
	var startSec, endSec int64
	if !start.IsZero() {
		startSec = start.Unix()
	}
	if !end.IsZero() {
		endSec = end.Unix()
	}

	type rowKey struct {
		api   string
		model string
	}
	acc := make(map[rowKey]*SummaryRow)

	a.mu.RLock()
	for k, b := range a.buckets {
		if b == nil {
			continue
		}
		bucketStart := k.hour * secondsPerHour
		bucketEnd := bucketStart + secondsPerHour
		if startSec != 0 && bucketEnd <= startSec {
			continue
		}
		if endSec != 0 && bucketStart >= endSec {
			continue
		}
		rk := rowKey{api: k.api, model: k.model}
		row := acc[rk]
		if row == nil {
			row = &SummaryRow{APIKey: k.api, Model: k.model}
			acc[rk] = row
		}
		row.Requests += b.Requests
		row.Failed += b.Failed
		row.InputTokens += b.InputTokens
		row.OutputTokens += b.OutputTokens
		row.ReasoningTokens += b.ReasoningTokens
		row.CachedTokens += b.CachedTokens
		row.TotalTokens += b.TotalTokens
	}
	a.mu.RUnlock()

	rows := make([]SummaryRow, 0, len(acc))
	for _, row := range acc {
		rows = append(rows, *row)
	}
	return rows
}

// Stop performs a final aggregation tick and closes the Redis client. Safe to
// call multiple times.
func (a *Aggregator) Stop() {
	if a == nil {
		return
	}
	a.once.Do(func() {
		if a.started.Load() {
			close(a.stopCh)
			<-a.doneCh
		}
		_ = a.client.Close()
	})
}
