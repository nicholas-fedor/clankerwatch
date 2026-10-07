// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
)

// testStart is when the daemon in these tests started.
var testStart = time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

// testNow is the decision time in these tests, an hour after the start.
var testNow = testStart.Add(time.Hour)

// testPolicy is the production schedule with the default intervals.
var testPolicy = DefaultPolicy(5*time.Minute, 20*time.Minute)

// errUnreadable stands in for a credentials file the daemon cannot read.
var errUnreadable = errors.New("permission denied")

// usableLogin returns a login that passes every gate at testNow.
//
// Returns:
//   - claudecode.OAuth: a token with the profile scope, valid for hours.
func usableLogin() claudecode.OAuth {
	return claudecode.OAuth{
		AccessToken:      "token",
		ExpiresAt:        testStart.Add(8 * time.Hour),
		LoginExpiresAt:   testStart.Add(30 * 24 * time.Hour),
		Scopes:           []string{"user:inference", profileScope},
		SubscriptionType: "max",
		RateLimitTier:    "default_claude_max_5x",
	}
}

// baseInputs returns inputs at testNow with a usable login and no history.
//
// Returns:
//   - *Inputs: inputs each case adjusts.
func baseInputs() *Inputs {
	return &Inputs{
		Now:       testNow,
		StartedAt: testStart,
		OAuth:     usableLogin(),
		Active:    true,
	}
}

// waitFor builds a gated decision without a planned fetch.
//
// Parameters:
//   - gate: the expected gate.
//   - wake: the expected wake time.
//
// Returns:
//   - Decision: the expected decision.
func waitFor(gate Gate, wake time.Time) Decision {
	return Decision{Gate: gate, WakeAt: wake}
}

// TestDecide covers each scheduling rule against a table of states.
//
// Every case starts from a usable login at testNow, an hour after the daemon
// started, so each one changes only what its rule looks at.
func TestDecide(t *testing.T) {
	t.Parallel()

	probe := testNow.Add(testPolicy.Probe)

	tests := []struct {
		mutate func(in *Inputs)
		name   string
		want   Decision
	}{
		{
			name:   "fresh data waits for the interval",
			mutate: func(in *Inputs) { in.DataAt = testNow.Add(-time.Minute) },
			want: Decision{
				WakeAt: probe, NextFetchAt: testNow.Add(4 * time.Minute),
				RefreshAllowedAt: testNow.Add(time.Minute),
			},
		},
		{
			name:   "due while active",
			mutate: func(in *Inputs) { in.DataAt = testNow.Add(-5 * time.Minute) },
			want: Decision{
				Fetch: true, WakeAt: testNow, NextFetchAt: testNow,
				RefreshAllowedAt: testNow.Add(-3 * time.Minute),
			},
		},
		{
			name: "idle uses the idle interval",
			mutate: func(in *Inputs) {
				in.Active = false
				in.DataAt = testNow.Add(-10 * time.Minute)
			},
			want: Decision{
				WakeAt: probe, NextFetchAt: testNow.Add(10 * time.Minute),
				RefreshAllowedAt: testNow.Add(-8 * time.Minute),
			},
		},
		{
			name:   "activity after an idle stretch fetches at once",
			mutate: func(in *Inputs) { in.DataAt = testNow.Add(-10 * time.Minute) },
			want: Decision{
				Fetch: true, WakeAt: testNow.Add(-5 * time.Minute), NextFetchAt: testNow.Add(-5 * time.Minute),
				RefreshAllowedAt: testNow.Add(-8 * time.Minute),
			},
		},
		{
			name:   "last attempt counts like data",
			mutate: func(in *Inputs) { in.LastAttempt = testNow.Add(-time.Minute) },
			want: Decision{
				WakeAt: probe, NextFetchAt: testNow.Add(4 * time.Minute),
				RefreshAllowedAt: testNow.Add(time.Minute),
			},
		},
		{
			name:   "first start waits for the startup delay",
			mutate: func(in *Inputs) { in.Now = testStart.Add(3 * time.Second) },
			want: Decision{
				WakeAt: testStart.Add(10 * time.Second), NextFetchAt: testStart.Add(10 * time.Second),
				RefreshAllowedAt: Anytime,
			},
		},
		{
			name:   "first start fetches after the startup delay",
			mutate: func(in *Inputs) { in.Now = testStart.Add(10 * time.Second) },
			want: Decision{
				Fetch: true, WakeAt: testStart.Add(10 * time.Second), NextFetchAt: testStart.Add(10 * time.Second),
				RefreshAllowedAt: Anytime,
			},
		},
		{
			name: "manual refresh on first start skips the startup delay",
			mutate: func(in *Inputs) {
				in.Now = testStart.Add(time.Second)
				in.Manual = true
			},
			want: Decision{
				Fetch: true, WakeAt: testStart.Add(time.Second), NextFetchAt: testStart.Add(time.Second),
				RefreshAllowedAt: Anytime,
			},
		},
		{
			name: "an upcoming reset waits for the grace period",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-2 * time.Minute)
				in.NextReset = testNow.Add(-30 * time.Second)
			},
			want: Decision{
				WakeAt: testNow.Add(15 * time.Second), NextFetchAt: testNow.Add(15 * time.Second),
				RefreshAllowedAt: testNow,
			},
		},
		{
			name: "a passed reset fetches after the grace period",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-2 * time.Minute)
				in.NextReset = testNow.Add(-time.Minute)
			},
			want: Decision{
				Fetch: true, WakeAt: testNow.Add(-15 * time.Second), NextFetchAt: testNow.Add(-15 * time.Second),
				RefreshAllowedAt: testNow,
			},
		},
		{
			name: "a distant reset does not delay the interval",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-time.Minute)
				in.NextReset = testNow.Add(3 * time.Hour)
			},
			want: Decision{
				WakeAt: probe, NextFetchAt: testNow.Add(4 * time.Minute),
				RefreshAllowedAt: testNow.Add(time.Minute),
			},
		},
		{
			name: "manual refresh inside the gap is ignored",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-time.Minute)
				in.Manual = true
			},
			want: Decision{
				WakeAt: probe, NextFetchAt: testNow.Add(4 * time.Minute),
				RefreshAllowedAt: testNow.Add(time.Minute),
			},
		},
		{
			name: "manual refresh after the gap fetches",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-3 * time.Minute)
				in.Manual = true
			},
			want: Decision{
				Fetch: true, WakeAt: testNow, NextFetchAt: testNow,
				RefreshAllowedAt: testNow.Add(-time.Minute),
			},
		},
		{
			name: "wall-clock jump fetches at once",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-6 * time.Hour)
				in.LastAttempt = testNow.Add(-6 * time.Hour)
			},
			want: Decision{
				Fetch: true, WakeAt: testNow.Add(-6*time.Hour + 5*time.Minute),
				NextFetchAt:      testNow.Add(-6*time.Hour + 5*time.Minute),
				RefreshAllowedAt: testNow.Add(-6*time.Hour + 2*time.Minute),
			},
		},
		{
			name: "backoff holds fetches and refreshes",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-10 * time.Minute)
				in.Manual = true
				in.Backoff = Backoff{Until: testNow.Add(10 * time.Minute), Kind: BackoffRateLimited, Consecutive: 1}
			},
			want: Decision{
				Gate: GateBackoff, WakeAt: probe, NextFetchAt: testNow.Add(10 * time.Minute),
				RefreshAllowedAt: testNow.Add(10 * time.Minute),
			},
		},
		{
			name: "backoff ending before the probe wakes at its end",
			mutate: func(in *Inputs) {
				in.Backoff = Backoff{Until: testNow.Add(time.Minute), Kind: BackoffServer, Consecutive: 1}
			},
			want: Decision{
				Gate: GateBackoff, WakeAt: testNow.Add(time.Minute), NextFetchAt: testNow.Add(time.Minute),
				RefreshAllowedAt: testNow.Add(time.Minute),
			},
		},
		{
			name: "expired backoff no longer gates",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-10 * time.Minute)
				in.Backoff = Backoff{Until: testNow.Add(-time.Second), Kind: BackoffServer, Consecutive: 3}
			},
			want: Decision{
				Fetch: true, WakeAt: testNow.Add(-5 * time.Minute), NextFetchAt: testNow.Add(-5 * time.Minute),
				RefreshAllowedAt: testNow.Add(-8 * time.Minute),
			},
		},
		{
			name: "failed attempt retries when its backoff ends",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-10 * time.Minute)
				in.LastAttempt = testNow.Add(-2 * time.Minute)
				in.Backoff = Backoff{Until: testNow.Add(-time.Second), Kind: BackoffServer, Consecutive: 1}
			},
			want: Decision{
				Fetch: true, WakeAt: testNow.Add(-time.Second), NextFetchAt: testNow.Add(-time.Second),
				RefreshAllowedAt: testNow,
			},
		},
		{
			name: "fresher data skips the failed attempt's retry",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-time.Minute)
				in.LastAttempt = testNow.Add(-2 * time.Minute)
				in.Backoff = Backoff{Until: testNow.Add(-time.Second), Kind: BackoffServer, Consecutive: 1}
			},
			want: Decision{
				Fetch: false, WakeAt: probe, NextFetchAt: testNow.Add(4 * time.Minute),
				RefreshAllowedAt: testNow.Add(time.Minute),
			},
		},
		{
			name:   "missing credentials",
			mutate: func(in *Inputs) { in.CredErr = claudecode.ErrNoCredentials },
			want:   waitFor(GateLoggedOut, probe),
		},
		{
			name:   "logged-out stub",
			mutate: func(in *Inputs) { in.CredErr = fmt.Errorf("read login: %w", claudecode.ErrLoggedOut) },
			want:   waitFor(GateLoggedOut, probe),
		},
		{
			name:   "unreadable credentials",
			mutate: func(in *Inputs) { in.CredErr = errUnreadable },
			want:   waitFor(GateCredentials, probe),
		},
		{
			name:   "missing profile scope",
			mutate: func(in *Inputs) { in.OAuth.Scopes = []string{"user:inference"} },
			want:   waitFor(GateScope, probe),
		},
		{
			name:   "token about to expire",
			mutate: func(in *Inputs) { in.OAuth.ExpiresAt = testNow.Add(4 * time.Minute) },
			want:   waitFor(GateTokenExpiring, probe),
		},
		{
			name:   "empty token counts as expiring",
			mutate: func(in *Inputs) { in.OAuth.AccessToken = "" },
			want:   waitFor(GateTokenExpiring, probe),
		},
		{
			name: "rejected token is blocked",
			mutate: func(in *Inputs) {
				in.AuthBlock = &AuthBlock{TokenExpiresAt: in.OAuth.ExpiresAt, Since: testNow.Add(-10 * time.Minute)}
			},
			want: waitFor(GateAuthBlocked, probe),
		},
		{
			name: "rejected token wakes for its retry",
			mutate: func(in *Inputs) {
				in.AuthBlock = &AuthBlock{TokenExpiresAt: in.OAuth.ExpiresAt, Since: testNow.Add(-59 * time.Minute)}
			},
			want: waitFor(GateAuthBlocked, testNow.Add(time.Minute)),
		},
		{
			name: "rejected token is retried after AuthRetry",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-10 * time.Minute)
				in.AuthBlock = &AuthBlock{TokenExpiresAt: in.OAuth.ExpiresAt, Since: testNow.Add(-time.Hour)}
			},
			want: Decision{
				Fetch: true, WakeAt: testNow.Add(-5 * time.Minute), NextFetchAt: testNow.Add(-5 * time.Minute),
				RefreshAllowedAt: testNow.Add(-8 * time.Minute),
			},
		},
		{
			name: "a new token lifts the block",
			mutate: func(in *Inputs) {
				in.DataAt = testNow.Add(-10 * time.Minute)
				in.AuthBlock = &AuthBlock{TokenExpiresAt: testStart, Since: testNow.Add(-time.Minute)}
			},
			want: Decision{
				Fetch: true, WakeAt: testNow.Add(-5 * time.Minute), NextFetchAt: testNow.Add(-5 * time.Minute),
				RefreshAllowedAt: testNow.Add(-8 * time.Minute),
			},
		},
		{
			name: "login gates come before the backoff",
			mutate: func(in *Inputs) {
				in.CredErr = claudecode.ErrNoCredentials
				in.Backoff = Backoff{Until: testNow.Add(time.Minute), Kind: BackoffServer, Consecutive: 1}
			},
			want: waitFor(GateLoggedOut, probe),
		},
		{
			name: "cache-only never fetches",
			mutate: func(in *Inputs) {
				in.CacheOnly = true
				in.Manual = true
				in.CredErr = claudecode.ErrNoCredentials
				in.DataAt = testNow.Add(-time.Hour)
			},
			want: Decision{WakeAt: probe, RefreshAllowedAt: Anytime},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := baseInputs()
			tt.mutate(in)

			assert.Equal(t, tt.want, Decide(testPolicy, in))
		})
	}
}

// TestEarliestIgnoresZeroTimes checks zero means "no time" rather than the past.
func TestEarliestIgnoresZeroTimes(t *testing.T) {
	t.Parallel()

	early, late := testStart, testNow

	tests := []struct {
		first  time.Time
		second time.Time
		want   time.Time
		name   string
	}{
		{name: "first earlier", first: early, second: late, want: early},
		{name: "second earlier", first: late, second: early, want: early},
		{name: "equal", first: early, second: early, want: early},
		{name: "first zero", first: time.Time{}, second: late, want: late},
		{name: "second zero", first: late, second: time.Time{}, want: late},
		{name: "both zero", first: time.Time{}, second: time.Time{}, want: time.Time{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Earliest(tt.first, tt.second))
		})
	}
}

// TestLatestPicksTheLaterTime checks the helper that combines data and attempt times.
func TestLatestPicksTheLaterTime(t *testing.T) {
	t.Parallel()

	assert.Equal(t, testNow, latest(testStart, testNow))
	assert.Equal(t, testNow, latest(testNow, testStart))
	assert.Equal(t, testNow, latest(time.Time{}, testNow))
	assert.True(t, latest(time.Time{}, time.Time{}).IsZero())
}
