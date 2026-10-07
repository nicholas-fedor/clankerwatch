// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestBackoffNextStartsAtTheBase checks the first failure waits the kind's base.
func TestBackoffNextStartsAtTheBase(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(5*time.Minute, 20*time.Minute)
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

	got := Backoff{}.Next(policy, BackoffServer, now, 0)

	assert.Equal(t, Backoff{
		Until:       now.Add(2 * time.Minute),
		Since:       now,
		Kind:        BackoffServer,
		Consecutive: 1,
	}, got)
}

// TestBackoffNextDoublesUpToTheCap checks consecutive failures grow the delay.
//
// The run keeps its original start time, which the status uses to decide
// whether newer data from Claude Code supersedes the failure.
func TestBackoffNextDoublesUpToTheCap(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(5*time.Minute, 20*time.Minute)
	start := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	want := []time.Duration{
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute,
	}

	backoff := Backoff{}
	now := start

	for index, delay := range want {
		backoff = backoff.Next(policy, BackoffNetwork, now, 0)

		assert.Equal(t, now.Add(delay), backoff.Until, "failure %d", index+1)
		assert.Equal(t, index+1, backoff.Consecutive)
		assert.Equal(t, start, backoff.Since)

		now = backoff.Until
	}
}

// TestBackoffNextHonorsRetryAfterForRateLimits checks Retry-After floors the delay.
//
// A Retry-After above the cap is still clamped, so a hostile or broken
// header cannot stall the daemon for days.
func TestBackoffNextHonorsRetryAfterForRateLimits(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(5*time.Minute, 20*time.Minute)
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		retryAfter time.Duration
		want       time.Duration
	}{
		{name: "no header uses the base", retryAfter: 0, want: 5 * time.Minute},
		{name: "short header keeps the base", retryAfter: 30 * time.Second, want: 5 * time.Minute},
		{name: "long header raises the delay", retryAfter: 10 * time.Minute, want: 10 * time.Minute},
		{name: "huge header is capped", retryAfter: 48 * time.Hour, want: time.Hour},
		{name: "negative header is ignored", retryAfter: -time.Hour, want: 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Backoff{}.Next(policy, BackoffRateLimited, now, tt.retryAfter)

			assert.Equal(t, now.Add(tt.want), got.Until)
		})
	}
}

// TestBackoffNextIgnoresRetryAfterForOtherKinds checks only rate limits use the header.
func TestBackoffNextIgnoresRetryAfterForOtherKinds(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(5*time.Minute, 20*time.Minute)
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

	for _, kind := range []BackoffKind{BackoffServer, BackoffBadPayload, BackoffNetwork} {
		got := Backoff{}.Next(policy, kind, now, 20*time.Minute)
		base, _ := policy.curve(kind)

		assert.Equal(t, now.Add(base), got.Until, "kind %s", kind)
	}
}

// TestBackoffNextResetsOnAnotherKind checks a new kind of failure starts a new run.
func TestBackoffNextResetsOnAnotherKind(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(5*time.Minute, 20*time.Minute)
	start := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	now := start.Add(time.Hour)
	previous := Backoff{Until: now, Since: start, Kind: BackoffNetwork, Consecutive: 4}

	got := previous.Next(policy, BackoffServer, now, 0)

	assert.Equal(t, Backoff{Until: now.Add(2 * time.Minute), Since: now, Kind: BackoffServer, Consecutive: 1}, got)
}

// TestBackoffNextRestartsAfterAZeroCount checks a cleared count starts over.
//
// A state file can hold a kind without a count, and that must not continue
// a run that never started.
func TestBackoffNextRestartsAfterAZeroCount(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(5*time.Minute, 20*time.Minute)
	start := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	now := start.Add(time.Hour)

	got := Backoff{Kind: BackoffServer, Since: start}.Next(policy, BackoffServer, now, 0)

	assert.Equal(t, 1, got.Consecutive)
	assert.Equal(t, now, got.Since)
}

// TestDelayClampsBetweenFloorAndCeiling covers the delay curve directly.
func TestDelayClampsBetweenFloorAndCeiling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     time.Duration
		ceiling  time.Duration
		floor    time.Duration
		failures int
		want     time.Duration
	}{
		{name: "first failure", base: time.Minute, ceiling: time.Hour, failures: 1, want: time.Minute},
		{name: "third failure", base: time.Minute, ceiling: time.Hour, failures: 3, want: 4 * time.Minute},
		{name: "capped", base: time.Minute, ceiling: 5 * time.Minute, failures: 10, want: 5 * time.Minute},
		{name: "many failures stop doubling", base: time.Minute, ceiling: time.Hour, failures: 1 << 30, want: time.Hour},
		{name: "floor raises", base: time.Minute, ceiling: time.Hour, floor: 7 * time.Minute, failures: 1, want: 7 * time.Minute},
		{name: "floor above ceiling", base: time.Minute, ceiling: time.Hour, floor: 2 * time.Hour, failures: 1, want: time.Hour},
		{name: "zero failures", base: time.Minute, ceiling: time.Hour, failures: 0, want: time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, delay(tt.base, tt.ceiling, tt.failures, tt.floor))
		})
	}
}
