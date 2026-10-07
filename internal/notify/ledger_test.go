// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// ledgerNow is the current time in ledger tests.
var ledgerNow = time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

// testThresholds are the warning and critical thresholds the tests use.
var testThresholds = []float64{80, 95}

// sessionBar returns a session bar at percent that resets in three hours.
//
// Parameters:
//   - percent: the percentage used.
//
// Returns:
//   - usage.Bar: the bar.
func sessionBar(percent float64) usage.Bar {
	return usage.Bar{
		ID:       "session",
		Kind:     "session",
		Label:    "Current session",
		Short:    "5h",
		Percent:  percent,
		ResetsAt: ledgerNow.Add(3 * time.Hour),
	}
}

// openBar returns a bar without a reset time.
//
// Parameters:
//   - percent: the percentage used.
//
// Returns:
//   - usage.Bar: the bar.
func openBar(percent float64) usage.Bar {
	return usage.Bar{ID: "weekly_scoped:model:fable", Label: "Fable", Percent: percent}
}

// recordAll evaluates bars and records every alert as delivered.
//
// Parameters:
//   - ledger: the ledger.
//   - now: the current time.
//   - bars: the current bars.
//
// Returns:
//   - []Alert: the alerts that were due.
func recordAll(ledger *Ledger, now time.Time, bars ...usage.Bar) []Alert {
	alerts := ledger.Evaluate(now, bars, testThresholds)
	for _, alert := range alerts {
		ledger.Record(alert, 0, now)
	}

	return alerts
}

// TestEvaluateBelowThresholds yields nothing under the lowest threshold.
func TestEvaluateBelowThresholds(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	assert.Empty(t, ledger.Evaluate(ledgerNow, []usage.Bar{sessionBar(79.9)}, testThresholds))
}

// TestEvaluateWithoutThresholds yields nothing but still prunes.
func TestEvaluateWithoutThresholds(t *testing.T) {
	t.Parallel()

	ledger := Ledger{Entries: []Entry{{Bar: "old", Threshold: 80, ResetsAt: ledgerNow.Add(-48 * time.Hour)}}}

	assert.Nil(t, ledger.Evaluate(ledgerNow, []usage.Bar{sessionBar(100)}, nil))
	assert.Empty(t, ledger.Entries)
}

// TestEvaluateCrossingEachThreshold alerts once per threshold as usage climbs.
func TestEvaluateCrossingEachThreshold(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	alerts := recordAll(&ledger, ledgerNow, sessionBar(81))
	require.Len(t, alerts, 1)
	assert.InDelta(t, 80.0, alerts[0].Threshold, 0)
	assert.False(t, alerts[0].Top)
	assert.Equal(t, []float64{80}, alerts[0].Covers)

	assert.Empty(t, recordAll(&ledger, ledgerNow.Add(time.Minute), sessionBar(90)))

	alerts = recordAll(&ledger, ledgerNow.Add(2*time.Minute), sessionBar(96))
	require.Len(t, alerts, 1)
	assert.InDelta(t, 95.0, alerts[0].Threshold, 0)
	assert.True(t, alerts[0].Top)
	assert.Equal(t, []float64{95}, alerts[0].Covers)

	assert.Empty(t, recordAll(&ledger, ledgerNow.Add(3*time.Minute), sessionBar(120)))
}

// TestEvaluateJumpCoversSkippedThresholds sends one alert for a jump from 50%
// to 97% that settles both thresholds.
func TestEvaluateJumpCoversSkippedThresholds(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	assert.Empty(t, recordAll(&ledger, ledgerNow, sessionBar(50)))

	alerts := recordAll(&ledger, ledgerNow.Add(time.Minute), sessionBar(97))
	require.Len(t, alerts, 1)
	assert.InDelta(t, 95.0, alerts[0].Threshold, 0)
	assert.True(t, alerts[0].Top)
	assert.Equal(t, []float64{80, 95}, alerts[0].Covers)
	assert.Len(t, ledger.Entries, 2)

	assert.Empty(t, recordAll(&ledger, ledgerNow.Add(2*time.Minute), sessionBar(99)))
}

// TestEvaluateResetJitterIsOneWindow treats reset times up to 30 minutes apart
// as the same window, and a later reset as a new one.
func TestEvaluateResetJitterIsOneWindow(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	require.Len(t, recordAll(&ledger, ledgerNow, sessionBar(85)), 1)

	for _, jitter := range []time.Duration{-30 * time.Minute, -time.Second, time.Second, 10 * time.Minute, 30 * time.Minute} {
		bar := sessionBar(85)
		bar.ResetsAt = bar.ResetsAt.Add(jitter)

		assert.Empty(t, ledger.Evaluate(ledgerNow, []usage.Bar{bar}, testThresholds), "jitter %v", jitter)
	}

	next := sessionBar(85)
	next.ResetsAt = next.ResetsAt.Add(5 * time.Hour)

	alerts := ledger.Evaluate(ledgerNow, []usage.Bar{next}, testThresholds)
	require.Len(t, alerts, 1)
	assert.Equal(t, []float64{80}, alerts[0].Covers)
}

// TestEvaluateSkipsCreditBars never alerts on a credit balance.
func TestEvaluateSkipsCreditBars(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	credit := usage.Bar{ID: "credit", Kind: "credit", Percent: 100, Credit: true}

	assert.Empty(t, ledger.Evaluate(ledgerNow, []usage.Bar{credit}, testThresholds))
}

// TestEvaluateSkipsFinishedWindows ignores a bar whose window already reset.
func TestEvaluateSkipsFinishedWindows(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	for _, resetsAt := range []time.Time{ledgerNow, ledgerNow.Add(-time.Minute)} {
		bar := sessionBar(99)
		bar.ResetsAt = resetsAt

		assert.Empty(t, ledger.Evaluate(ledgerNow, []usage.Bar{bar}, testThresholds))
	}
}

// TestEvaluateOneAlertPerBar alerts every bar independently.
func TestEvaluateOneAlertPerBar(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	alerts := recordAll(&ledger, ledgerNow, sessionBar(85), openBar(99), sessionBar(10))
	require.Len(t, alerts, 2)
	assert.Equal(t, "session", alerts[0].Bar.ID)
	assert.Equal(t, "weekly_scoped:model:fable", alerts[1].Bar.ID)
	assert.Equal(t, []float64{80, 95}, alerts[1].Covers)
}

// TestEvaluateRearmsBarsWithoutReset re-arms a threshold once usage drops more
// than five points below it.
func TestEvaluateRearmsBarsWithoutReset(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	require.Len(t, recordAll(&ledger, ledgerNow, openBar(82)), 1)

	// 75 is exactly five points below, which is not far enough.
	assert.Empty(t, recordAll(&ledger, ledgerNow, openBar(75)))
	assert.Len(t, ledger.Entries, 1)
	assert.Empty(t, recordAll(&ledger, ledgerNow, openBar(82)))

	assert.Empty(t, recordAll(&ledger, ledgerNow, openBar(74.9)))
	assert.Empty(t, ledger.Entries)

	alerts := recordAll(&ledger, ledgerNow, openBar(82))
	require.Len(t, alerts, 1)
	assert.Equal(t, []float64{80}, alerts[0].Covers)
}

// TestPrune drops entries a day after their window ended and keeps the rest.
func TestPrune(t *testing.T) {
	t.Parallel()

	ended := ledgerNow.Add(-keepFor - time.Second)
	recent := ledgerNow.Add(-keepFor)

	ledger := Ledger{Entries: []Entry{
		{Bar: "session", Threshold: 80, ResetsAt: ended},
		{Bar: "session", Threshold: 80, ResetsAt: recent},
		{Bar: "weekly", Threshold: 80, ResetsAt: ledgerNow.Add(time.Hour)},
		{Bar: "absent", Threshold: 80},
		{Bar: openBar(0).ID, Threshold: 95},
	}}

	ledger.prune(ledgerNow, []usage.Bar{openBar(50)})

	assert.Equal(t, []Entry{
		{Bar: "session", Threshold: 80, ResetsAt: recent},
		{Bar: "weekly", Threshold: 80, ResetsAt: ledgerNow.Add(time.Hour)},
		{Bar: "absent", Threshold: 80},
	}, ledger.Entries)
}

// TestRecord stores one entry per covered threshold and the replacement ID.
func TestRecord(t *testing.T) {
	t.Parallel()

	var ledger Ledger

	bar := sessionBar(97)
	alert := Alert{Bar: bar, Threshold: 95, Top: true, Covers: []float64{80, 95}}

	ledger.Record(alert, 0, ledgerNow)
	assert.Nil(t, ledger.ReplaceIDs)
	assert.Equal(t, []Entry{
		{Bar: "session", Threshold: 80, ResetsAt: bar.ResetsAt, At: ledgerNow},
		{Bar: "session", Threshold: 95, ResetsAt: bar.ResetsAt, At: ledgerNow},
	}, ledger.Entries)

	ledger.Record(alert, 42, ledgerNow)
	assert.Equal(t, map[string]uint32{"session": 42}, ledger.ReplaceIDs)

	ledger.Record(alert, 43, ledgerNow)
	assert.Equal(t, map[string]uint32{"session": 43}, ledger.ReplaceIDs)
}

// TestSameWindow matches reset times within the tolerance.
func TestSameWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		first  time.Time
		second time.Time
		name   string
		want   bool
	}{
		{name: "both zero", want: true},
		{name: "first zero", second: ledgerNow},
		{name: "second zero", first: ledgerNow},
		{name: "identical", first: ledgerNow, second: ledgerNow, want: true},
		{name: "at the tolerance", first: ledgerNow, second: ledgerNow.Add(windowTolerance), want: true},
		{name: "at the negative tolerance", first: ledgerNow.Add(windowTolerance), second: ledgerNow, want: true},
		{name: "past the tolerance", first: ledgerNow, second: ledgerNow.Add(windowTolerance + time.Nanosecond)},
		{name: "past the negative tolerance", first: ledgerNow.Add(windowTolerance + time.Nanosecond), second: ledgerNow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, sameWindow(tt.first, tt.second))
		})
	}
}
