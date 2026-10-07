// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLevelOf covers threshold grading and the server's severity override.
//
// The server can know a limit is in trouble before the percentage says so, so
// its severity may raise the level. It must never lower it, or a bar past the
// critical threshold would be shown as normal.
func TestLevelOf(t *testing.T) {
	t.Parallel()

	const warn, crit = 80.0, 95.0

	tests := []struct {
		name     string
		severity string
		percent  float64
		want     Level
	}{
		{name: "zero", percent: 0, want: LevelNormal},
		{name: "just below warning", percent: 79.99, want: LevelNormal},
		{name: "at warning", percent: 80, want: LevelWarning},
		{name: "just below critical", percent: 94.99, want: LevelWarning},
		{name: "at critical", percent: 95, want: LevelCritical},
		{name: "past the limit", percent: 140, want: LevelCritical},
		{name: "negative percent", percent: -5, want: LevelNormal},
		{name: "server normal", severity: "normal", percent: 10, want: LevelNormal},
		{name: "server warning raises", severity: "warning", percent: 10, want: LevelWarning},
		{name: "server critical raises", severity: "critical", percent: 10, want: LevelCritical},
		{name: "server severity ignores case", severity: "CrItIcAl", percent: 10, want: LevelCritical},
		{name: "server warning never lowers", severity: "warning", percent: 99, want: LevelCritical},
		{name: "server normal never lowers", severity: "normal", percent: 99, want: LevelCritical},
		{name: "unknown severity ignored", severity: "panic", percent: 85, want: LevelWarning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bar := Bar{Percent: tt.percent, Severity: tt.severity}
			assert.Equal(t, tt.want, LevelOf(bar, warn, crit))
		})
	}
}

// TestLevelOrdering checks the levels rise with urgency.
//
// LevelOf relies on max to pick the more urgent grading, which only works
// while the constants stay in increasing order.
func TestLevelOrdering(t *testing.T) {
	t.Parallel()

	assert.Less(t, LevelNormal, LevelWarning)
	assert.Less(t, LevelWarning, LevelCritical)
}
