// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import (
	"errors"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
)

// Gate names what currently blocks a fetch.
type Gate uint8

// Inputs is everything Decide looks at.
type Inputs struct {
	// Backoff is the failure state.
	Backoff Backoff

	// Now is the current wall-clock time.
	Now time.Time

	// DataAt is when the current data was fetched, or zero.
	DataAt time.Time

	// LastAttempt is when the endpoint was last called, or zero.
	LastAttempt time.Time

	// StartedAt is when the daemon started.
	StartedAt time.Time

	// NextReset is the earliest bar reset after DataAt, or zero.
	NextReset time.Time

	// CredErr is the error from reading the login, or nil.
	CredErr error

	// AuthBlock is the rejected token, or nil.
	AuthBlock *AuthBlock

	// OAuth is the login.
	OAuth claudecode.OAuth

	// CacheOnly disables fetching.
	CacheOnly bool

	// Active reports Claude Code activity.
	Active bool

	// Manual reports that the widget asked for a refresh.
	Manual bool
}

// Decision is what to do now and when to look again.
type Decision struct {
	// WakeAt is when to decide again.
	WakeAt time.Time

	// NextFetchAt is the planned fetch, or zero while waiting on something
	// outside the daemon.
	NextFetchAt time.Time

	// RefreshAllowedAt is when a widget refresh is honored, or zero while a
	// refresh cannot help.
	RefreshAllowedAt time.Time

	// Fetch tells the caller to call the endpoint now.
	Fetch bool

	// Gate is what blocks fetching, or GateNone.
	Gate Gate
}

// Gates in the order Decide checks them.
const (
	// GateNone means nothing blocks fetching.
	GateNone Gate = iota

	// GateLoggedOut means there are no credentials or only a logged-out stub.
	GateLoggedOut

	// GateCredentials means the credentials file cannot be read.
	GateCredentials

	// GateScope means the token lacks the profile scope.
	GateScope

	// GateAuthBlocked means the server rejected this token recently.
	GateAuthBlocked

	// GateTokenExpiring means the token expires soon and Claude Code will refresh it.
	GateTokenExpiring

	// GateBackoff means a failure or a Retry-After is being waited out.
	GateBackoff
)

// profileScope is the OAuth scope the usage endpoint requires.
const profileScope = "user:profile"

// Anytime is the refresh time that means "allowed now". A fixed value keeps
// the snapshot from changing on every wake.
var Anytime = time.UnixMilli(0)

// Decide applies the scheduling rules.
//
// Parameters:
//   - policy: the schedule.
//   - in: the current state.
//
// Returns:
//   - Decision: whether to fetch now and when to decide again.
func Decide(policy Policy, in *Inputs) Decision {
	if in.CacheOnly {
		return Decision{
			Fetch: false, Gate: GateNone, WakeAt: in.Now.Add(policy.Probe),
			NextFetchAt: time.Time{}, RefreshAllowedAt: Anytime,
		}
	}

	decision, blocked := gated(policy, in)
	if blocked {
		return decision
	}

	return plan(policy, in)
}

// gated checks the gates that block fetching.
//
// Parameters:
//   - policy: the schedule.
//   - in: the current state.
//
// Returns:
//   - Decision: the blocked decision.
//   - bool: true when a gate applies.
func gated(policy Policy, in *Inputs) (Decision, bool) {
	probe := in.Now.Add(policy.Probe)

	gate := loginGate(in)
	if gate != GateNone {
		return blocked(gate, probe), true
	}

	if block := in.AuthBlock; block != nil && block.TokenExpiresAt.Equal(in.OAuth.ExpiresAt) {
		retry := block.Since.Add(policy.AuthRetry)
		if in.Now.Before(retry) {
			return blocked(GateAuthBlocked, Earliest(probe, retry)), true
		}
	}

	if !in.OAuth.UsableAt(in.Now, policy.ExpirySkew) {
		return blocked(GateTokenExpiring, probe), true
	}

	until := in.Backoff.Until
	if in.Now.Before(until) {
		return Decision{
			Fetch: false, Gate: GateBackoff, WakeAt: Earliest(probe, until),
			NextFetchAt: until, RefreshAllowedAt: until,
		}, true
	}

	return Decision{}, false
}

// loginGate checks whether the login can be used at all.
//
// Parameters:
//   - in: the current state.
//
// Returns:
//   - Gate: GateLoggedOut, GateCredentials, GateScope, or GateNone.
func loginGate(in *Inputs) Gate {
	switch {
	case errors.Is(in.CredErr, claudecode.ErrNoCredentials), errors.Is(in.CredErr, claudecode.ErrLoggedOut):
		return GateLoggedOut
	case in.CredErr != nil:
		return GateCredentials
	case !in.OAuth.HasScope(profileScope):
		return GateScope
	default:
		return GateNone
	}
}

// blocked builds a decision that waits on something outside the daemon.
//
// Parameters:
//   - gate: the blocking gate.
//   - wake: when to decide again.
//
// Returns:
//   - Decision: a decision without a planned fetch or refresh.
func blocked(gate Gate, wake time.Time) Decision {
	return Decision{Fetch: false, Gate: gate, WakeAt: wake, NextFetchAt: time.Time{}, RefreshAllowedAt: time.Time{}}
}

// plan schedules the next fetch when no gate applies.
//
// Parameters:
//   - policy: the schedule.
//   - in: the current state.
//
// Returns:
//   - Decision: whether to fetch now and when to decide again.
func plan(policy Policy, in *Inputs) Decision {
	interval := policy.IdleInterval
	if in.Active {
		interval = policy.Interval
	}

	last := latest(in.DataAt, in.LastAttempt)
	due, refreshAt := last.Add(interval), last.Add(policy.ManualGap)

	if last.IsZero() {
		due, refreshAt = in.StartedAt.Add(policy.StartupDelay), Anytime
	}

	if !in.NextReset.IsZero() {
		due = Earliest(due, in.NextReset.Add(policy.ResetGrace))
	}

	// A failed attempt is retried when its backoff ends. The curves put
	// transient failures below the polling interval, and a newer result,
	// such as Claude Code's cache, makes the retry unnecessary.
	if in.Backoff.Consecutive > 0 && !in.DataAt.After(in.LastAttempt) {
		due = Earliest(due, in.Backoff.Until)
	}

	if in.Manual && !in.Now.Before(refreshAt) {
		due = in.Now
	}

	return Decision{
		Fetch:            !in.Now.Before(due),
		Gate:             GateNone,
		WakeAt:           Earliest(in.Now.Add(policy.Probe), due),
		NextFetchAt:      due,
		RefreshAllowedAt: refreshAt,
	}
}

// Earliest returns the earlier non-zero time.
//
// Parameters:
//   - first: a time, possibly zero.
//   - second: a time, possibly zero.
//
// Returns:
//   - [time.Time]: the earlier of the two, ignoring zero values.
func Earliest(first, second time.Time) time.Time {
	if first.IsZero() || (!second.IsZero() && second.Before(first)) {
		return second
	}

	return first
}

// latest returns the later time.
//
// Parameters:
//   - first: a time.
//   - second: a time.
//
// Returns:
//   - [time.Time]: the later of the two.
func latest(first, second time.Time) time.Time {
	if second.After(first) {
		return second
	}

	return first
}
