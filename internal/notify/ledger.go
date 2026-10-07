// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// Entry records that a threshold alert was delivered for one window of a bar.
type Entry struct {
	// ResetsAt identifies the window, or is zero for a bar without resets.
	ResetsAt time.Time `json:"resetsAt,omitzero"`

	// At is when the alert was delivered.
	At time.Time `json:"at"`

	// Bar is the bar ID.
	Bar string `json:"bar"`

	// Threshold is the alerted threshold in percent.
	Threshold float64 `json:"threshold"`
}

// Ledger remembers delivered alerts so they are never repeated.
type Ledger struct {
	// ReplaceIDs maps a bar ID to its last notification, which the next alert
	// for that bar replaces.
	ReplaceIDs map[string]uint32 `json:"replaceIDs,omitempty"`

	// Auth is the key of the last login problem alerted.
	Auth string `json:"auth,omitempty"`

	// Entries are the delivered threshold alerts.
	Entries []Entry `json:"entries,omitempty"`
}

// Alert is one notification to send.
type Alert struct {
	// Covers lists every threshold this alert settles.
	Covers []float64

	// Bar is the bar that crossed a threshold.
	Bar usage.Bar

	// Threshold is the threshold the notification reports.
	Threshold float64

	// Top reports that Threshold is the highest configured threshold.
	Top bool
}

const (
	// windowTolerance treats reset times this close together as one window.
	// Fetches report the same reset with a little jitter, and real windows are
	// hours apart.
	windowTolerance = 30 * time.Minute

	// rearmMargin re-arms a threshold on a bar without a reset time once usage
	// drops this far below it.
	rearmMargin = 5.0

	// keepFor is how long entries for a finished window are remembered.
	keepFor = 24 * time.Hour
)

// Evaluate prunes stale entries and returns the alerts due now.
//
// Credit bars and windows that already reset are skipped. A bar past several
// unalerted thresholds yields one alert for the highest of them.
//
// Parameters:
//   - now: the current time.
//   - bars: the current bars.
//   - thresholds: the alert thresholds, ascending.
//
// Returns:
//   - []Alert: the alerts to send, one per bar at most.
func (l *Ledger) Evaluate(now time.Time, bars []usage.Bar, thresholds []float64) []Alert {
	l.prune(now, bars)

	if len(thresholds) == 0 {
		return nil
	}

	top := thresholds[len(thresholds)-1]

	var alerts []Alert

	for _, bar := range bars {
		if bar.Credit || (!bar.ResetsAt.IsZero() && !bar.ResetsAt.After(now)) {
			continue
		}

		pending := l.pending(bar, thresholds)
		if len(pending) == 0 {
			continue
		}

		highest := pending[len(pending)-1]

		alerts = append(alerts, Alert{Bar: bar, Threshold: highest, Top: highest == top, Covers: pending})
	}

	return alerts
}

// Record marks an alert as delivered.
//
// Parameters:
//   - alert: the delivered alert.
//   - id: the notification ID the server assigned, or 0.
//   - now: the delivery time.
func (l *Ledger) Record(alert Alert, id uint32, now time.Time) {
	for _, threshold := range alert.Covers {
		entry := Entry{Bar: alert.Bar.ID, Threshold: threshold, ResetsAt: alert.Bar.ResetsAt, At: now}

		l.Entries = append(l.Entries, entry)
	}

	if id == 0 {
		return
	}

	if l.ReplaceIDs == nil {
		l.ReplaceIDs = make(map[string]uint32)
	}

	l.ReplaceIDs[alert.Bar.ID] = id
}

// has reports whether a threshold was alerted for the bar's current window.
//
// Parameters:
//   - bar: the bar.
//   - threshold: the threshold.
//
// Returns:
//   - bool: true when an entry exists.
func (l *Ledger) has(bar usage.Bar, threshold float64) bool {
	for _, entry := range l.Entries {
		if entry.Bar == bar.ID && entry.Threshold == threshold && sameWindow(entry.ResetsAt, bar.ResetsAt) {
			return true
		}
	}

	return false
}

// pending returns the thresholds a bar has passed without an alert.
//
// Parameters:
//   - bar: the bar.
//   - thresholds: the alert thresholds, ascending.
//
// Returns:
//   - []float64: the unalerted thresholds at or below the bar's percentage.
func (l *Ledger) pending(bar usage.Bar, thresholds []float64) []float64 {
	var pending []float64

	for _, threshold := range thresholds {
		if bar.Percent >= threshold && !l.has(bar, threshold) {
			pending = append(pending, threshold)
		}
	}

	return pending
}

// prune drops entries for windows that ended a day ago, and re-arms
// thresholds on bars without resets once usage dropped well below them.
//
// Parameters:
//   - now: the current time.
//   - bars: the current bars.
func (l *Ledger) prune(now time.Time, bars []usage.Bar) {
	percent := make(map[string]float64, len(bars))

	for _, bar := range bars {
		if bar.ResetsAt.IsZero() {
			percent[bar.ID] = bar.Percent
		}
	}

	kept := l.Entries[:0]

	for _, entry := range l.Entries {
		if !entry.ResetsAt.IsZero() && now.Sub(entry.ResetsAt) > keepFor {
			continue
		}

		current, ok := percent[entry.Bar]
		if ok && entry.ResetsAt.IsZero() && current < entry.Threshold-rearmMargin {
			continue
		}

		kept = append(kept, entry)
	}

	clear(l.Entries[len(kept):])

	l.Entries = kept
}

// sameWindow reports whether two reset times identify the same window.
//
// Parameters:
//   - first: one reset time.
//   - second: the other reset time.
//
// Returns:
//   - bool: true when both are zero, or both are set and close together.
func sameWindow(first, second time.Time) bool {
	if first.IsZero() || second.IsZero() {
		return first.IsZero() && second.IsZero()
	}

	diff := first.Sub(second)

	return diff <= windowTolerance && diff >= -windowTolerance
}
