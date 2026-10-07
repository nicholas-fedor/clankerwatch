// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package schedule_test drives Decide and Backoff.Next the way the engine does.
package schedule_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
)

// simulation replays the engine's loop against a scripted endpoint.
type simulation struct {
	start   time.Time
	now     time.Time
	fetches []time.Time
	in      schedule.Inputs
	policy  schedule.Policy
}

// minStep is the shortest simulated sleep, matching the engine's.
const minStep = time.Second

// newSimulation starts a simulation with a usable login and no history.
//
// Parameters:
//   - active: whether Claude Code is in use.
//
// Returns:
//   - *simulation: the simulation at its start time.
func newSimulation(active bool) *simulation {
	start := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

	return &simulation{
		start:  start,
		now:    start,
		policy: schedule.DefaultPolicy(5*time.Minute, 20*time.Minute),
		in: schedule.Inputs{
			StartedAt: start,
			Active:    active,
			OAuth: claudecode.OAuth{
				AccessToken: "token",
				ExpiresAt:   start.Add(48 * time.Hour),
				Scopes:      []string{"user:profile"},
			},
		},
	}
}

// run advances the simulation until the deadline.
//
// Parameters:
//   - until: how long after the start to stop.
//   - respond: the endpoint, given the fetch number from 0. It returns
//     whether the fetch succeeded and the Retry-After of a rate limit.
func (s *simulation) run(until time.Duration, respond func(n int) (bool, time.Duration)) {
	for s.now.Before(s.start.Add(until)) {
		s.in.Now = s.now

		decision := schedule.Decide(s.policy, &s.in)
		if decision.Fetch {
			ok, retryAfter := respond(len(s.fetches))
			s.fetches = append(s.fetches, s.now)
			s.in.LastAttempt = s.now

			if ok {
				s.in.DataAt, s.in.Backoff = s.now, schedule.Backoff{}
			} else {
				s.in.Backoff = s.in.Backoff.Next(s.policy, schedule.BackoffRateLimited, s.now, retryAfter)
			}

			s.in.Now = s.now
			decision = schedule.Decide(s.policy, &s.in)
		}

		s.now = s.now.Add(max(decision.WakeAt.Sub(s.now), minStep))
	}
}

// TestSimulatedDayKeepsTheInterval checks a healthy endpoint is polled on cadence.
//
// The first fetch waits for the startup delay, and every later one follows
// the interval exactly, so probe wakes never add requests.
func TestSimulatedDayKeepsTheInterval(t *testing.T) {
	t.Parallel()

	sim := newSimulation(true)
	sim.run(time.Hour, func(int) (bool, time.Duration) { return true, 0 })

	require.NotEmpty(t, sim.fetches)
	assert.Equal(t, sim.start.Add(10*time.Second), sim.fetches[0])
	assert.Len(t, sim.fetches, 12)

	for index := 1; index < len(sim.fetches); index++ {
		assert.Equal(t, 5*time.Minute, sim.fetches[index].Sub(sim.fetches[index-1]), "fetch %d", index)
	}
}

// TestSimulatedIdleUsesTheIdleInterval checks an idle session is polled slowly.
func TestSimulatedIdleUsesTheIdleInterval(t *testing.T) {
	t.Parallel()

	sim := newSimulation(false)
	sim.run(time.Hour, func(int) (bool, time.Duration) { return true, 0 })

	require.Len(t, sim.fetches, 3)
	assert.Equal(t, 20*time.Minute, sim.fetches[1].Sub(sim.fetches[0]))
	assert.Equal(t, 20*time.Minute, sim.fetches[2].Sub(sim.fetches[1]))
}

// TestSimulatedRateLimitBacksOff checks a rate-limited endpoint is left alone.
//
// Each retry waits at least the Retry-After and the doubling curve, and the
// interval resumes once a fetch succeeds.
func TestSimulatedRateLimitBacksOff(t *testing.T) {
	t.Parallel()

	sim := newSimulation(true)
	sim.run(2*time.Hour, func(n int) (bool, time.Duration) {
		if n < 3 {
			return false, 10 * time.Minute
		}

		return true, 0
	})

	require.GreaterOrEqual(t, len(sim.fetches), 5)

	gaps := []time.Duration{
		sim.fetches[1].Sub(sim.fetches[0]),
		sim.fetches[2].Sub(sim.fetches[1]),
		sim.fetches[3].Sub(sim.fetches[2]),
		sim.fetches[4].Sub(sim.fetches[3]),
	}

	assert.Equal(t, []time.Duration{10 * time.Minute, 10 * time.Minute, 20 * time.Minute, 5 * time.Minute}, gaps)
}
