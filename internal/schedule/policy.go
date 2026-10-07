// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import "time"

// Policy holds the scheduling constants.
type Policy struct {
	// Interval is the fetch cadence while Claude Code is active.
	Interval time.Duration

	// IdleInterval is the fetch cadence while Claude Code is idle.
	IdleInterval time.Duration

	// Probe is the longest sleep between looks at the local files.
	Probe time.Duration

	// ManualGap is the minimum spacing of widget-requested refreshes.
	ManualGap time.Duration

	// ExpirySkew is how close to expiry a token is left to Claude Code.
	ExpirySkew time.Duration

	// ResetGrace is the delay after a window resets before fetching.
	ResetGrace time.Duration

	// AuthRetry is the retry spacing for a rejected token.
	AuthRetry time.Duration

	// StartupDelay is the delay before the first network attempt.
	StartupDelay time.Duration

	// RateLimitBase is the first backoff after rate limiting.
	RateLimitBase time.Duration

	// RateLimitCap is the longest backoff after rate limiting.
	RateLimitCap time.Duration

	// ServerErrorBase is the first backoff after a server error.
	ServerErrorBase time.Duration

	// ServerErrorCap is the longest backoff after a server error.
	ServerErrorCap time.Duration

	// NetworkBase is the first backoff after a transport error.
	NetworkBase time.Duration

	// NetworkCap is the longest backoff after a transport error.
	NetworkCap time.Duration
}

// Production schedule.
const (
	defaultProbe        = 2 * time.Minute
	defaultManualGap    = 2 * time.Minute
	defaultExpirySkew   = 5 * time.Minute
	defaultResetGrace   = 45 * time.Second
	defaultAuthRetry    = time.Hour
	defaultStartupDelay = 10 * time.Second
	rateLimitBase       = 5 * time.Minute
	rateLimitCap        = time.Hour
	serverErrorBase     = 2 * time.Minute
	serverErrorCap      = 30 * time.Minute
	networkBase         = time.Minute
	networkCap          = 10 * time.Minute
)

// DefaultPolicy returns the production schedule for the given intervals.
//
// Parameters:
//   - interval: the fetch cadence while Claude Code is active.
//   - idle: the fetch cadence while Claude Code is idle.
//
// Returns:
//   - Policy: the schedule.
func DefaultPolicy(interval, idle time.Duration) Policy {
	return Policy{
		Interval:        interval,
		IdleInterval:    idle,
		Probe:           defaultProbe,
		ManualGap:       defaultManualGap,
		ExpirySkew:      defaultExpirySkew,
		ResetGrace:      defaultResetGrace,
		AuthRetry:       defaultAuthRetry,
		StartupDelay:    defaultStartupDelay,
		RateLimitBase:   rateLimitBase,
		RateLimitCap:    rateLimitCap,
		ServerErrorBase: serverErrorBase,
		ServerErrorCap:  serverErrorCap,
		NetworkBase:     networkBase,
		NetworkCap:      networkCap,
	}
}

// curve returns the backoff base and cap for a failure kind.
//
// Parameters:
//   - kind: the failure kind.
//
// Returns:
//   - base: the first delay.
//   - ceiling: the longest delay.
//
//nolint:nonamedreturns // Same-type returns need names.
func (p Policy) curve(kind BackoffKind) (base, ceiling time.Duration) {
	switch kind {
	case BackoffRateLimited:
		return p.RateLimitBase, p.RateLimitCap
	case BackoffNetwork:
		return p.NetworkBase, p.NetworkCap
	case BackoffServer, BackoffBadPayload:
		return p.ServerErrorBase, p.ServerErrorCap
	default:
		return p.ServerErrorBase, p.ServerErrorCap
	}
}
