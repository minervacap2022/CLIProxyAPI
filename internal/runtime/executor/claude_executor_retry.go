package executor

import (
	"net/http"
	"strconv"
	"time"
)

func parseAnthropicRetryAfter(header http.Header) *time.Duration {
	resetUnix, err := strconv.ParseInt(header.Get("anthropic-ratelimit-unified-reset"), 10, 64)
	if err != nil {
		return nil
	}
	duration := time.Until(time.Unix(resetUnix, 0))
	if duration <= 0 {
		return nil
	}
	return &duration
}

func newClaudeStatusError(code int, message string, header http.Header) statusErr {
	err := statusErr{code: code, msg: message}
	if code == http.StatusTooManyRequests {
		err.retryAfter = parseAnthropicRetryAfter(header)
		err.accountQuota = err.retryAfter != nil
	}
	return err
}
