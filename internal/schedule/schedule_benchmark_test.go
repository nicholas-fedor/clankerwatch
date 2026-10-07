// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule_test

import (
	"testing"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
)

// BenchmarkDecide measures one decision on the common path.
//
// The inputs pass every gate and carry data, an attempt, and a reset, so the
// planner does all of its work.
//
// Parameters:
//   - b: benchmark handle.
func BenchmarkDecide(b *testing.B) {
	policy := schedule.DefaultPolicy(5*time.Minute, 20*time.Minute)
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	in := &schedule.Inputs{
		Now:         now,
		DataAt:      now.Add(-3 * time.Minute),
		LastAttempt: now.Add(-3 * time.Minute),
		StartedAt:   now.Add(-time.Hour),
		NextReset:   now.Add(time.Hour),
		OAuth: claudecode.OAuth{
			AccessToken: "token",
			ExpiresAt:   now.Add(8 * time.Hour),
			Scopes:      []string{"user:inference", "user:profile"},
		},
		Active: true,
	}

	var sink schedule.Decision

	b.ReportAllocs()

	for b.Loop() {
		sink = schedule.Decide(policy, in)
	}

	if sink.WakeAt.IsZero() {
		b.Fatal("zero wake time")
	}
}
