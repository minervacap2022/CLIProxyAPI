// Package management consumes this server-side aggregation to power the
// management panel's "Team" usage view. The legacy /usage endpoint returns the
// full snapshot including every per-request detail, which grows unbounded and
// forces the browser to download and aggregate megabytes of data. SummaryRows
// performs the same api_key × model grouping in Go and returns only the
// aggregated rows for the requested time window, keeping the payload small.
package usage

import "time"

// SummaryRow is an aggregated usage record grouped by API key and model.
type SummaryRow struct {
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	Requests        int64  `json:"requests"`
	Failed          int64  `json:"failed"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ReasoningTokens int64  `json:"reasoning_tokens"`
	CachedTokens    int64  `json:"cached_tokens"`
	TotalTokens     int64  `json:"total_tokens"`
}

// SummaryRows aggregates per-request details into api_key × model rows, filtered
// to the [start, end] window. Zero-value bounds are treated as unbounded.
func (s *RequestStatistics) SummaryRows(start, end time.Time) []SummaryRow {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	type key struct {
		api   string
		model string
	}
	acc := make(map[key]*SummaryRow)

	for apiName, stats := range s.apis {
		if stats == nil {
			continue
		}
		for modelName, modelStatsValue := range stats.Models {
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
				k := key{api: apiName, model: modelName}
				row, ok := acc[k]
				if !ok {
					row = &SummaryRow{APIKey: apiName, Model: modelName}
					acc[k] = row
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

	rows := make([]SummaryRow, 0, len(acc))
	for _, row := range acc {
		rows = append(rows, *row)
	}
	return rows
}
