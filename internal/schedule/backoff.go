// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import "time"

// BackoffKind names the failure a backoff is waiting out.
type BackoffKind string

// Backoff is the failure state that delays the next fetch.
type Backoff struct {
	// Until is when fetching may resume.
	Until time.Time `json:"until,omitzero"`

	// Since is when this run of failures began.
	Since time.Time `json:"since,omitzero"`

	// Kind is the failure being waited out.
	Kind BackoffKind `json:"kind,omitempty"`

	// Consecutive counts failures of Kind in a row.
	Consecutive int `json:"consecutive,omitempty"`
}

// AuthBlock records that the server rejected a token.
//
// A new token always has a new expiry, so the expiry identifies the token
// without storing it.
type AuthBlock struct {
	// TokenExpiresAt identifies the rejected token.
	TokenExpiresAt time.Time `json:"tokenExpiresAt"`

	// Since is when the token was rejected.
	Since time.Time `json:"since"`

	// Reason is unauthorized or scope.
	Reason string `json:"reason"`
}

// Failure kinds with their own backoff curves.
const (
	// BackoffRateLimited follows a 429, floored by Retry-After.
	BackoffRateLimited BackoffKind = "rate_limited"

	// BackoffServer follows a server error.
	BackoffServer BackoffKind = "server"

	// BackoffBadPayload follows a payload the parser does not recognize.
	BackoffBadPayload BackoffKind = "bad_payload"

	// BackoffNetwork follows a transport error.
	BackoffNetwork BackoffKind = "network"
)

// Next returns the backoff after another failure of kind at now.
//
// The delay doubles from the kind's base on each consecutive failure, up to
// its cap. Only rate limits use the server's Retry-After, as a floor.
//
// Parameters:
//   - policy: the schedule with the backoff curves.
//   - kind: the failure.
//   - now: the failure time.
//   - retryAfter: the server's Retry-After, or 0.
//
// Returns:
//   - Backoff: the new backoff state.
func (b Backoff) Next(policy Policy, kind BackoffKind, now time.Time, retryAfter time.Duration) Backoff {
	failures, since := 1, now
	if b.Kind == kind && b.Consecutive > 0 {
		failures, since = b.Consecutive+1, b.Since
	}

	floor := time.Duration(0)
	if kind == BackoffRateLimited {
		floor = retryAfter
	}

	base, ceiling := policy.curve(kind)

	until := now.Add(delay(base, ceiling, failures, floor))

	return Backoff{Until: until, Kind: kind, Consecutive: failures, Since: since}
}

// delay is the wait after the nth consecutive failure.
//
// Parameters:
//   - base: the first delay.
//   - ceiling: the longest delay.
//   - failures: consecutive failures, starting at 1.
//   - floor: the shortest acceptable delay.
//
// Returns:
//   - [time.Duration]: the delay, between floor and ceiling.
func delay(base, ceiling time.Duration, failures int, floor time.Duration) time.Duration {
	wait := base

	for i := 1; i < failures && wait < ceiling; i++ {
		wait *= 2
	}

	return min(ceiling, max(floor, wait))
}
