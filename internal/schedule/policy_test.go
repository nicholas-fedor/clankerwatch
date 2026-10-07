// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDefaultPolicyKeepsIntervals checks the configured cadences reach the policy.
//
// The intervals come from the user's config, so a policy that dropped them
// would poll at the wrong rate without any visible error.
func TestDefaultPolicyKeepsIntervals(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(3*time.Minute, 30*time.Minute)

	assert.Equal(t, 3*time.Minute, policy.Interval)
	assert.Equal(t, 30*time.Minute, policy.IdleInterval)
}

// TestDefaultPolicyUsesProductionConstants pins the production schedule.
//
// These values are the contract with the usage endpoint's rate limits, so a
// change to any of them must be deliberate.
func TestDefaultPolicyUsesProductionConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Policy{
		Interval:        5 * time.Minute,
		IdleInterval:    20 * time.Minute,
		Probe:           2 * time.Minute,
		ManualGap:       2 * time.Minute,
		ExpirySkew:      5 * time.Minute,
		ResetGrace:      45 * time.Second,
		AuthRetry:       time.Hour,
		StartupDelay:    10 * time.Second,
		RateLimitBase:   5 * time.Minute,
		RateLimitCap:    time.Hour,
		ServerErrorBase: 2 * time.Minute,
		ServerErrorCap:  30 * time.Minute,
		NetworkBase:     time.Minute,
		NetworkCap:      10 * time.Minute,
	}, DefaultPolicy(5*time.Minute, 20*time.Minute))
}

// TestCurveSelectsTheKindsBackoff checks each failure kind gets its own curve.
//
// Bad payloads share the server curve, and an unknown kind falls back to it
// so a state file from a newer version still backs off.
func TestCurveSelectsTheKindsBackoff(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy(5*time.Minute, 20*time.Minute)

	tests := []struct {
		name     string
		kind     BackoffKind
		wantBase time.Duration
		wantCap  time.Duration
	}{
		{name: "rate limited", kind: BackoffRateLimited, wantBase: 5 * time.Minute, wantCap: time.Hour},
		{name: "network", kind: BackoffNetwork, wantBase: time.Minute, wantCap: 10 * time.Minute},
		{name: "server", kind: BackoffServer, wantBase: 2 * time.Minute, wantCap: 30 * time.Minute},
		{name: "bad payload", kind: BackoffBadPayload, wantBase: 2 * time.Minute, wantCap: 30 * time.Minute},
		{name: "unknown kind", kind: BackoffKind("future"), wantBase: 2 * time.Minute, wantCap: 30 * time.Minute},
		{name: "empty kind", kind: "", wantBase: 2 * time.Minute, wantCap: 30 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base, ceiling := policy.curve(tt.kind)

			assert.Equal(t, tt.wantBase, base)
			assert.Equal(t, tt.wantCap, ceiling)
		})
	}
}
