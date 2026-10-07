// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// TestPlanName turns subscriptions and tiers into labels.
func TestPlanName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		subscription string
		tier         string
		want         string
	}{
		{name: "max 5x", subscription: "max", tier: "default_claude_max_5x", want: "Max 5x"},
		{name: "max 20x", subscription: "max", tier: "default_claude_max_20x", want: "Max 20x"},
		{name: "pro without a multiplier", subscription: "pro", tier: "default_claude_pro", want: "Pro"},
		{name: "no tier", subscription: "team", tier: "", want: "Team"},
		{name: "case and spaces normalized", subscription: "  ENTERPRISE ", tier: "", want: "Enterprise"},
		{name: "multiplier only at the end", subscription: "max", tier: "5x_default", want: "Max"},
		{name: "non-ASCII first letter", subscription: "équipe", tier: "", want: "Équipe"},
		{name: "empty subscription", subscription: "", tier: "default_claude_max_5x", want: ""},
		{name: "blank subscription", subscription: " \t", tier: "default_claude_max_5x", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, planName(tt.subscription, tt.tier))
		})
	}
}

// TestMillisKeepsZeroAsZero checks an unset time is 0, not a year-1 timestamp.
func TestMillisKeepsZeroAsZero(t *testing.T) {
	t.Parallel()

	assert.Zero(t, millis(time.Time{}))
	assert.Zero(t, millis(schedule.Anytime))
	assert.Equal(t, testNow.UnixMilli(), millis(testNow))
}

// TestBarsJSONGradesEachBar checks levels follow the thresholds and the server's severity.
func TestBarsJSONGradesEachBar(t *testing.T) {
	t.Parallel()

	bars := []usage.Bar{
		{ID: "a", Percent: 10, ResetsAt: testNow},
		{ID: "b", Percent: 85},
		{ID: "c", Percent: 95, Headline: true},
		{ID: "d", Percent: 10, Severity: "Critical", Credit: true},
	}

	got := barsJSON(bars, 80, 95)

	require.Len(t, got, 4)
	assert.Equal(t, barJSON{ID: "a", Percent: 10, ResetsAtMs: testNow.UnixMilli(), Level: usage.LevelNormal}, got[0])
	assert.Equal(t, usage.LevelWarning, got[1].Level)
	assert.Zero(t, got[1].ResetsAtMs)
	assert.Equal(t, usage.LevelCritical, got[2].Level)
	assert.True(t, got[2].Headline)
	assert.Equal(t, usage.LevelCritical, got[3].Level)
	assert.True(t, got[3].Credit)

	assert.NotNil(t, barsJSON(nil, 80, 95))
	assert.Empty(t, barsJSON(nil, 80, 95))
}

// TestExtraToJSON renders extra usage with its level.
func TestExtraToJSON(t *testing.T) {
	t.Parallel()

	percent := func(value float64) *float64 { return &value }
	dollars := func(minor int64) *usage.Money { return &usage.Money{Minor: minor, Decimals: 2, Currency: "USD"} }

	tests := []struct {
		extra *usage.Extra
		want  *extraJSON
		name  string
	}{
		{name: "absent", extra: nil, want: nil},
		{
			name:  "amounts and percent",
			extra: &usage.Extra{Percent: percent(25), Used: dollars(1250), Limit: dollars(5000)},
			want:  &extraJSON{Percent: percent(25), Used: "$12.50", Limit: "$50.00", Level: usage.LevelNormal},
		},
		{
			name:  "warning percent",
			extra: &usage.Extra{Percent: percent(85)},
			want:  &extraJSON{Percent: percent(85), Level: usage.LevelWarning},
		},
		{
			name:  "limit reached is critical",
			extra: &usage.Extra{Percent: percent(10), LimitReached: true},
			want:  &extraJSON{Percent: percent(10), Level: usage.LevelCritical, LimitReached: true},
		},
		{
			name:  "no percent",
			extra: &usage.Extra{Used: dollars(0)},
			want:  &extraJSON{Used: "$0.00", Level: usage.LevelNormal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, extraToJSON(tt.extra, 80, 95))
		})
	}
}

// TestEncodeSnapshotUsesEmptyArrays checks the widget never sees "bars": null.
func TestEncodeSnapshotUsesEmptyArrays(t *testing.T) {
	t.Parallel()

	got := encodeSnapshot(&snapshot{V: SnapshotVersion, Status: StatusStarting})

	assert.Contains(t, got, `"bars":[]`)
	assert.Contains(t, got, `"refreshAllowedAtMs":null`)
	assert.Contains(t, got, `"extraUsage":null`)
	assert.Contains(t, got, `"v":1`)
}

// TestBuildSnapshotReflectsTheEngine checks the snapshot carries the engine state.
func TestBuildSnapshotReflectsTheEngine(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, testOptions(), Deps{})
	e.oauth = testLogin(testNow.Add(time.Hour))
	withData(t, e, testPayload(t, 85, 10, testNow.Add(time.Hour)), testNow)
	e.decision = schedule.Decision{NextFetchAt: testNow.Add(5 * time.Minute), RefreshAllowedAt: testNow.Add(2 * time.Minute)}

	got := e.buildSnapshot()

	assert.Contains(t, got, `"plan":"Max 5x"`)
	assert.Contains(t, got, `"source":"api"`)
	assert.Contains(t, got, `"status":"ok"`)
	assert.Contains(t, got, `"refreshAllowedAtMs":1791288120000`)
	assert.Contains(t, got, `"nextUpdateAtMs":1791288300000`)
	assert.Contains(t, got, `"fetchedAtMs":1791288000000`)
	assert.NotContains(t, got, "secret-token")
}
