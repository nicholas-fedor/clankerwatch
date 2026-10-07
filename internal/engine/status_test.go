// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
)

// TestStatusFor maps every engine state to the status the widget shows.
//
// Claude Code's own data newer than the start of a failure run is current,
// so it reports ok even while the backoff keeps running.
func TestStatusFor(t *testing.T) {
	t.Parallel()

	since := testNow.Add(-10 * time.Minute)
	apiData := &state.Data{FetchedAt: testNow, Source: sourceAPI}
	freshCache := &state.Data{FetchedAt: testNow, Source: sourceClaudeCode}
	staleCache := &state.Data{FetchedAt: since.Add(-time.Minute), Source: sourceClaudeCode}

	backoff := func(kind schedule.BackoffKind) schedule.Backoff {
		return schedule.Backoff{Until: testNow.Add(time.Minute), Since: since, Kind: kind, Consecutive: 1}
	}

	tests := []struct {
		data        *state.Data
		name        string
		mode        config.Mode
		wantStatus  Status
		wantMessage string
		backoff     schedule.Backoff
		gate        schedule.Gate
	}{
		{
			name: "cache-only with data", mode: config.ModeCacheOnly, gate: schedule.GateNone, data: freshCache,
			wantStatus: StatusOK,
		},
		{
			name: "cache-only without data", mode: config.ModeCacheOnly, gate: schedule.GateNone,
			wantStatus: StatusNoData, wantMessage: "Claude Code has not cached any usage yet",
		},
		{
			name: "logged out", mode: config.ModeHybrid, gate: schedule.GateLoggedOut, data: apiData,
			wantStatus: StatusLoggedOut, wantMessage: "Claude Code is not signed in",
		},
		{
			name: "unreadable credentials", mode: config.ModeHybrid, gate: schedule.GateCredentials,
			wantStatus: StatusAuthError, wantMessage: "Claude Code's credentials cannot be read",
		},
		{
			name: "missing scope", mode: config.ModeHybrid, gate: schedule.GateScope,
			wantStatus: StatusAuthError, wantMessage: "Claude Code's login lacks the user:profile scope",
		},
		{
			name: "rejected token", mode: config.ModeHybrid, gate: schedule.GateAuthBlocked,
			wantStatus: StatusAuthError, wantMessage: "The usage endpoint rejected Claude Code's login",
		},
		{
			name: "expiring token", mode: config.ModeHybrid, gate: schedule.GateTokenExpiring, data: apiData,
			wantStatus: StatusTokenExpired, wantMessage: "Waiting for Claude Code to refresh its login",
		},
		{
			name: "rate limited", mode: config.ModeHybrid, gate: schedule.GateBackoff,
			backoff: backoff(schedule.BackoffRateLimited), data: apiData,
			wantStatus: StatusRateLimited, wantMessage: "The usage endpoint is rate limited",
		},
		{
			name: "offline", mode: config.ModeHybrid, gate: schedule.GateBackoff, backoff: backoff(schedule.BackoffNetwork),
			wantStatus: StatusOffline, wantMessage: "api.anthropic.com cannot be reached",
		},
		{
			name: "bad payload", mode: config.ModeHybrid, gate: schedule.GateBackoff, backoff: backoff(schedule.BackoffBadPayload),
			wantStatus: StatusError, wantMessage: "The usage endpoint returned an unrecognized payload",
		},
		{
			name: "server error", mode: config.ModeHybrid, gate: schedule.GateBackoff, backoff: backoff(schedule.BackoffServer),
			wantStatus: StatusError, wantMessage: "The usage endpoint returned an error",
		},
		{
			name: "unknown backoff kind", mode: config.ModeHybrid, gate: schedule.GateBackoff, backoff: backoff("future"),
			wantStatus: StatusError, wantMessage: "The usage endpoint returned an error",
		},
		{
			name: "fresh Claude Code data during a backoff", mode: config.ModeHybrid, gate: schedule.GateBackoff,
			backoff: backoff(schedule.BackoffRateLimited), data: freshCache, wantStatus: StatusOK,
		},
		{
			name: "stale Claude Code data during a backoff", mode: config.ModeHybrid, gate: schedule.GateBackoff,
			backoff: backoff(schedule.BackoffRateLimited), data: staleCache,
			wantStatus: StatusRateLimited, wantMessage: "The usage endpoint is rate limited",
		},
		{name: "data", mode: config.ModeHybrid, gate: schedule.GateNone, data: apiData, wantStatus: StatusOK},
		{name: "no data yet", mode: config.ModeHybrid, gate: schedule.GateNone, wantStatus: StatusStarting},
		{name: "unknown gate", mode: config.ModeHybrid, gate: schedule.Gate(200), wantStatus: StatusStarting},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, message := statusFor(tt.mode, tt.gate, tt.backoff, tt.data)

			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantMessage, message)
		})
	}
}

// TestLoginStatusIgnoresOtherGates checks only login gates map to a login status.
func TestLoginStatusIgnoresOtherGates(t *testing.T) {
	t.Parallel()

	for _, gate := range []schedule.Gate{schedule.GateNone, schedule.GateBackoff, schedule.Gate(200)} {
		_, ok := loginStatus(gate)

		assert.False(t, ok, "gate %d", gate)
	}
}
