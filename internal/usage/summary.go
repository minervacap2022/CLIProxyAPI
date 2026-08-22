// Package usage exposes compact server-side usage summaries for management views.
package usage

import (
	"sort"
	"time"
)

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

// SummaryRows aggregates per-request details into API-key-by-model rows.
// Zero-value time bounds are treated as unbounded.
func (s *RequestStatistics) SummaryRows(start, end time.Time) []SummaryRow {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	type summaryKey struct {
		apiKey string
		model  string
	}
	rowsByKey := make(map[summaryKey]*SummaryRow)

	for apiKey, api := range s.apis {
		if api == nil {
			continue
		}
		for model, modelStatsValue := range api.Models {
			if modelStatsValue == nil {
				continue
			}
			for index := range modelStatsValue.Details {
				detail := &modelStatsValue.Details[index]
				if !start.IsZero() && detail.Timestamp.Before(start) {
					continue
				}
				if !end.IsZero() && detail.Timestamp.After(end) {
					continue
				}

				key := summaryKey{apiKey: apiKey, model: model}
				row := rowsByKey[key]
				if row == nil {
					row = &SummaryRow{APIKey: apiKey, Model: model}
					rowsByKey[key] = row
				}

				row.Requests++
				if detail.Failed {
					row.Failed++
				}
				row.InputTokens += detail.Tokens.InputTokens
				row.OutputTokens += detail.Tokens.OutputTokens
				row.ReasoningTokens += detail.Tokens.ReasoningTokens
				row.CachedTokens += detail.Tokens.CachedTokens
				row.TotalTokens += detail.Tokens.TotalTokens
			}
		}
	}

	rows := make([]SummaryRow, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].APIKey == rows[right].APIKey {
			return rows[left].Model < rows[right].Model
		}
		return rows[left].APIKey < rows[right].APIKey
	})
	return rows
}
