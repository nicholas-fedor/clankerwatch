// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestParseRetryAfter covers both header forms and the bounds on the delay.
//
// A delay that is absent, zero, past, or malformed reports false so the
// caller keeps its own backoff. A huge delay is capped, so a hostile or
// broken server cannot park the daemon for days.
func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	date := func(offset time.Duration) string {
		return now.Add(offset).Format(http.TimeFormat)
	}

	tests := []struct {
		name   string
		value  string
		want   time.Duration
		wantOK bool
	}{
		{name: "empty", value: ""},
		{name: "blank", value: "   "},
		{name: "zero seconds", value: "0"},
		{name: "negative seconds", value: "-5"},
		{name: "seconds", value: "30", want: 30 * time.Second, wantOK: true},
		{name: "padded seconds", value: " 30 ", want: 30 * time.Second, wantOK: true},
		{name: "exactly one day", value: "86400", want: 24 * time.Hour, wantOK: true},
		{name: "past one day", value: "86401", want: 24 * time.Hour, wantOK: true},
		{name: "largest int64", value: "9223372036854775807", want: 24 * time.Hour, wantOK: true},
		{name: "beyond int64", value: "99999999999999999999"},
		{name: "fractional seconds", value: "1.5"},
		{name: "text", value: "soon"},
		{name: "future date", value: date(90 * time.Second), want: 90 * time.Second, wantOK: true},
		{name: "date now", value: date(0)},
		{name: "past date", value: date(-time.Minute)},
		{name: "far future date", value: date(72 * time.Hour), want: 24 * time.Hour, wantOK: true},
		{name: "RFC 850 date", value: now.Add(time.Hour).Format(time.RFC850), want: time.Hour, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseRetryAfter(tt.value, now)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSecondsDelay covers the delta-seconds conversion on its own.
func TestSecondsDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seconds int64
		want    time.Duration
		wantOK  bool
	}{
		{name: "negative", seconds: -1},
		{name: "zero", seconds: 0},
		{name: "one", seconds: 1, want: time.Second, wantOK: true},
		{name: "cap", seconds: 86400, want: maxRetryAfter, wantOK: true},
		{name: "over cap", seconds: 86401, want: maxRetryAfter, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := secondsDelay(tt.seconds)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
