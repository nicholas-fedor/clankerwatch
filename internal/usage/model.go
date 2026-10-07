// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"strings"
	"time"
)

// Level grades how close a limit is to being exhausted.
type Level uint8

// Bar is one usage limit as Claude Code's /usage view shows it.
type Bar struct {
	// ResetsAt is when the window resets, or zero when it has no reset time.
	ResetsAt time.Time

	// ID is a stable identifier, such as session or weekly_scoped:model:fable.
	ID string

	// Kind is the server's kind: session, weekly_all, weekly_scoped, or credit.
	Kind string

	// Group is the server's grouping, such as session or weekly.
	Group string

	// Label is the long label, such as "Current week (all models)".
	Label string

	// Short is the panel label, such as 7d.
	Short string

	// Severity is the server's grading: normal, warning, or critical.
	Severity string

	// Percent is the percentage used, which can exceed 100.
	Percent float64

	// Headline marks the bar a single-value indicator shows.
	Headline bool

	// Credit marks a credit balance, which is shown but never alerted on.
	Credit bool
}

// Usage is the parsed content of one usage payload.
type Usage struct {
	// Extra is pay-as-you-go usage, or nil while it is disabled.
	Extra *Extra

	// Bars are the limits in display order.
	Bars []Bar
}

// Extra describes pay-as-you-go usage beyond the plan limits.
type Extra struct {
	// Percent is the percentage of the monthly limit, or nil when unknown.
	Percent *float64

	// Used is the amount spent this month.
	Used *Money

	// Limit is the monthly limit, or nil when there is none.
	Limit *Money

	// DisabledReason is the server's reason when extra usage is unavailable.
	DisabledReason string

	// LimitReached reports that the monthly limit is exhausted.
	LimitReached bool
}

// Levels in increasing order of urgency.
const (
	// LevelNormal is below every threshold.
	LevelNormal Level = iota

	// LevelWarning is at or above the warning threshold.
	LevelWarning

	// LevelCritical is at or above the critical threshold.
	LevelCritical
)

// Server severities.
const (
	severityWarning  = "warning"
	severityCritical = "critical"
)

// LevelOf grades a bar by the configured thresholds and by the server's own
// severity, whichever is more urgent.
//
// Parameters:
//   - bar: the bar to grade.
//   - warn: the warning threshold in percent.
//   - crit: the critical threshold in percent.
//
// Returns:
//   - Level: the more urgent of the two gradings.
func LevelOf(bar Bar, warn, crit float64) Level {
	var level Level

	switch {
	case bar.Percent >= crit:
		level = LevelCritical
	case bar.Percent >= warn:
		level = LevelWarning
	default:
		level = LevelNormal
	}

	switch strings.ToLower(bar.Severity) {
	case severityCritical:
		level = max(level, LevelCritical)
	case severityWarning:
		level = max(level, LevelWarning)
	default:
	}

	return level
}
