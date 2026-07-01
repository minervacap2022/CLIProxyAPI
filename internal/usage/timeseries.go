package usage

import (
	"sort"
	"strings"
	"time"
)

// UsageBucket controls the output granularity for usage timeseries responses.
type UsageBucket string

const (
	UsageBucketAuto UsageBucket = "auto"
	UsageBucketHour UsageBucket = "hour"
	UsageBucketDay  UsageBucket = "day"
)

// TimeseriesPoint is an aggregated usage datapoint for a single time bucket.
type TimeseriesPoint struct {
	Start           time.Time `json:"start"`
	Requests        int64     `json:"requests"`
	Failed          int64     `json:"failed"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
}

// TimeseriesRow preserves API-key attribution for a single time bucket so the
// frontend can apply the same label-resolution rules as the Team view.
type TimeseriesRow struct {
	Start           time.Time `json:"start"`
	APIKey          string    `json:"api_key"`
	Requests        int64     `json:"requests"`
	Failed          int64     `json:"failed"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
}

type timeseriesRowKey struct {
	start time.Time
	api   string
}

// ResolveUsageBucket normalizes an input bucket value and expands "auto" to a
// concrete granularity using the requested time range.
func ResolveUsageBucket(value string, start, end time.Time) UsageBucket {
	switch UsageBucket(strings.ToLower(strings.TrimSpace(value))) {
	case UsageBucketHour:
		return UsageBucketHour
	case UsageBucketDay:
		return UsageBucketDay
	default:
	}

	if start.IsZero() || end.IsZero() {
		return UsageBucketHour
	}
	if end.Sub(start) > 96*time.Hour {
		return UsageBucketDay
	}
	return UsageBucketHour
}

func bucketStart(ts time.Time, bucket UsageBucket) time.Time {
	ts = ts.UTC()
	switch bucket {
	case UsageBucketDay:
		return time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return ts.Truncate(time.Hour)
	}
}

// Timeseries aggregates all usage details into time buckets over the requested
// range. This is the in-memory fallback when the hourly Redis aggregator is not
// configured.
func (s *RequestStatistics) Timeseries(start, end time.Time, bucket UsageBucket) []TimeseriesPoint {
	if s == nil {
		return nil
	}
	bucket = ResolveUsageBucket(string(bucket), start, end)

	acc := make(map[time.Time]*TimeseriesPoint)

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, stats := range s.apis {
		if stats == nil {
			continue
		}
		for _, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			for i := range modelStatsValue.Details {
				detail := &modelStatsValue.Details[i]
				ts := detail.Timestamp
				if !start.IsZero() && ts.Before(start) {
					continue
				}
				if !end.IsZero() && ts.After(end) {
					continue
				}
				key := bucketStart(ts, bucket)
				point := acc[key]
				if point == nil {
					point = &TimeseriesPoint{Start: key}
					acc[key] = point
				}
				point.Requests++
				if detail.Failed {
					point.Failed++
				}
				tokens := detail.Tokens
				point.InputTokens += tokens.InputTokens
				point.OutputTokens += tokens.OutputTokens
				point.ReasoningTokens += tokens.ReasoningTokens
				point.CachedTokens += tokens.CachedTokens
				point.TotalTokens += tokens.TotalTokens
			}
		}
	}

	return sortTimeseriesPoints(acc)
}

// TimeseriesRows aggregates usage details into api_key × time-bucket rows over
// the requested range.
func (s *RequestStatistics) TimeseriesRows(start, end time.Time, bucket UsageBucket) []TimeseriesRow {
	if s == nil {
		return nil
	}
	bucket = ResolveUsageBucket(string(bucket), start, end)

	acc := make(map[timeseriesRowKey]*TimeseriesRow)

	s.mu.RLock()
	defer s.mu.RUnlock()

	for apiName, stats := range s.apis {
		if stats == nil {
			continue
		}
		for _, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			for i := range modelStatsValue.Details {
				detail := &modelStatsValue.Details[i]
				ts := detail.Timestamp
				if !start.IsZero() && ts.Before(start) {
					continue
				}
				if !end.IsZero() && ts.After(end) {
					continue
				}
				rowStart := bucketStart(ts, bucket)
				key := timeseriesRowKey{start: rowStart, api: apiName}
				row := acc[key]
				if row == nil {
					row = &TimeseriesRow{Start: rowStart, APIKey: apiName}
					acc[key] = row
				}
				row.Requests++
				if detail.Failed {
					row.Failed++
				}
				tokens := detail.Tokens
				row.InputTokens += tokens.InputTokens
				row.OutputTokens += tokens.OutputTokens
				row.ReasoningTokens += tokens.ReasoningTokens
				row.CachedTokens += tokens.CachedTokens
				row.TotalTokens += tokens.TotalTokens
			}
		}
	}

	return sortTimeseriesRows(acc)
}

// Timeseries aggregates hourly buckets into either hourly or daily points over
// the requested range.
func (a *Aggregator) Timeseries(start, end time.Time, bucket UsageBucket) []TimeseriesPoint {
	if a == nil {
		return nil
	}
	bucket = ResolveUsageBucket(string(bucket), start, end)

	var startSec, endSec int64
	if !start.IsZero() {
		startSec = start.Unix()
	}
	if !end.IsZero() {
		endSec = end.Unix()
	}

	acc := make(map[time.Time]*TimeseriesPoint)

	a.mu.RLock()
	defer a.mu.RUnlock()

	for key, b := range a.buckets {
		if b == nil {
			continue
		}
		hourStart := time.Unix(key.hour*secondsPerHour, 0).UTC()
		hourStartSec := hourStart.Unix()
		hourEndSec := hourStartSec + secondsPerHour
		if startSec != 0 && hourEndSec <= startSec {
			continue
		}
		if endSec != 0 && hourStartSec > endSec {
			continue
		}
		pointKey := bucketStart(hourStart, bucket)
		point := acc[pointKey]
		if point == nil {
			point = &TimeseriesPoint{Start: pointKey}
			acc[pointKey] = point
		}
		point.Requests += b.Requests
		point.Failed += b.Failed
		point.InputTokens += b.InputTokens
		point.OutputTokens += b.OutputTokens
		point.ReasoningTokens += b.ReasoningTokens
		point.CachedTokens += b.CachedTokens
		point.TotalTokens += b.TotalTokens
	}

	return sortTimeseriesPoints(acc)
}

// TimeseriesRows aggregates hourly buckets into api_key × time-bucket rows.
func (a *Aggregator) TimeseriesRows(start, end time.Time, bucket UsageBucket) []TimeseriesRow {
	if a == nil {
		return nil
	}
	bucket = ResolveUsageBucket(string(bucket), start, end)

	var startSec, endSec int64
	if !start.IsZero() {
		startSec = start.Unix()
	}
	if !end.IsZero() {
		endSec = end.Unix()
	}

	acc := make(map[timeseriesRowKey]*TimeseriesRow)

	a.mu.RLock()
	defer a.mu.RUnlock()

	for key, b := range a.buckets {
		if b == nil {
			continue
		}
		hourStart := time.Unix(key.hour*secondsPerHour, 0).UTC()
		hourStartSec := hourStart.Unix()
		hourEndSec := hourStartSec + secondsPerHour
		if startSec != 0 && hourEndSec <= startSec {
			continue
		}
		if endSec != 0 && hourStartSec > endSec {
			continue
		}
		rowStart := bucketStart(hourStart, bucket)
		accKey := timeseriesRowKey{start: rowStart, api: key.api}
		row := acc[accKey]
		if row == nil {
			row = &TimeseriesRow{Start: rowStart, APIKey: key.api}
			acc[accKey] = row
		}
		row.Requests += b.Requests
		row.Failed += b.Failed
		row.InputTokens += b.InputTokens
		row.OutputTokens += b.OutputTokens
		row.ReasoningTokens += b.ReasoningTokens
		row.CachedTokens += b.CachedTokens
		row.TotalTokens += b.TotalTokens
	}

	return sortTimeseriesRows(acc)
}

func sortTimeseriesPoints(acc map[time.Time]*TimeseriesPoint) []TimeseriesPoint {
	points := make([]TimeseriesPoint, 0, len(acc))
	for _, point := range acc {
		points = append(points, *point)
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Start.Before(points[j].Start)
	})
	return points
}

func sortTimeseriesRows(acc map[timeseriesRowKey]*TimeseriesRow) []TimeseriesRow {
	rows := make([]TimeseriesRow, 0, len(acc))
	for _, row := range acc {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Start.Equal(rows[j].Start) {
			return rows[i].APIKey < rows[j].APIKey
		}
		return rows[i].Start.Before(rows[j].Start)
	})
	return rows
}
