// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRetryAfter bounds the delay accepted from a Retry-After header.
const maxRetryAfter = 24 * time.Hour

// ParseRetryAfter reads a Retry-After header given in seconds or as an HTTP
// date.
//
// Parameters:
//   - value: the header value.
//   - now: the current time, for HTTP dates.
//
// Returns:
//   - [time.Duration]: the delay, at most 24 hours.
//   - bool: false when the header is absent, zero, in the past, or malformed,
//     so the caller falls back to its own backoff.
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	seconds, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return secondsDelay(seconds)
	}

	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}

	delay := date.Sub(now)
	if delay <= 0 {
		return 0, false
	}

	return min(delay, maxRetryAfter), true
}

// secondsDelay converts a delta-seconds Retry-After value.
//
// Parameters:
//   - seconds: the header value in seconds.
//
// Returns:
//   - [time.Duration]: the delay, at most 24 hours.
//   - bool: false for zero or a negative value.
func secondsDelay(seconds int64) (time.Duration, bool) {
	if seconds <= 0 {
		return 0, false
	}

	if seconds > int64(maxRetryAfter/time.Second) {
		return maxRetryAfter, true
	}

	return time.Duration(seconds) * time.Second, true
}
