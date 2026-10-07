// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// Status is the daemon's overall state as the widget shows it.
type Status string

// snapshot is the JSON document the D-Bus Snapshot property carries.
//
// Times are epoch milliseconds. No field changes on every wake, so an
// unchanged snapshot is never re-sent.
type snapshot struct {
	RefreshAllowedAtMs *int64      `json:"refreshAllowedAtMs"`
	ExtraUsage         *extraJSON  `json:"extraUsage"`
	Mode               config.Mode `json:"mode"`
	Source             string      `json:"source"`
	Plan               string      `json:"plan"`
	Message            string      `json:"message"`
	Status             Status      `json:"status"`
	Bars               []barJSON   `json:"bars"`
	V                  int         `json:"v"`
	FetchedAtMs        int64       `json:"fetchedAtMs"`
	NextUpdateAtMs     int64       `json:"nextUpdateAtMs"`
	LoginExpiresAtMs   int64       `json:"loginExpiresAtMs"`
	WarnAt             float64     `json:"warnAt"`
	CritAt             float64     `json:"critAt"`
	Active             bool        `json:"active"`
}

// barJSON is one bar in the snapshot.
type barJSON struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Group      string      `json:"group"`
	Label      string      `json:"label"`
	Short      string      `json:"short"`
	Severity   string      `json:"severity"`
	Percent    float64     `json:"percent"`
	ResetsAtMs int64       `json:"resetsAtMs"`
	Level      usage.Level `json:"level"`
	Headline   bool        `json:"headline"`
	Credit     bool        `json:"credit"`
}

// extraJSON is the extra usage section of the snapshot.
type extraJSON struct {
	Percent      *float64    `json:"percent"`
	Used         string      `json:"used"`
	Limit        string      `json:"limit"`
	Level        usage.Level `json:"level"`
	LimitReached bool        `json:"limitReached"`
}

// SnapshotVersion is the schema version of the JSON snapshot.
const SnapshotVersion = 1

// Statuses published in the snapshot.
const (
	// StatusStarting means no data and no attempt yet.
	StatusStarting Status = "starting"

	// StatusOK means the data is current.
	StatusOK Status = "ok"

	// StatusRateLimited means the endpoint is rate limited.
	StatusRateLimited Status = "rate_limited"

	// StatusOffline means the endpoint cannot be reached.
	StatusOffline Status = "offline"

	// StatusError means the endpoint returned an error or an unknown payload.
	StatusError Status = "error"

	// StatusTokenExpired means the daemon waits for Claude Code to refresh its login.
	StatusTokenExpired Status = "token_expired"

	// StatusLoggedOut means Claude Code is not signed in.
	StatusLoggedOut Status = "logged_out"

	// StatusAuthError means the login cannot be used.
	StatusAuthError Status = "auth_error"

	// StatusNoData means cache-only mode has nothing cached.
	StatusNoData Status = "no_data"
)

// Data sources.
const (
	sourceAPI        = "api"
	sourceClaudeCode = "claude-code"
)

// tierMultiplier finds the usage multiplier at the end of a rate limit tier.
var tierMultiplier = regexp.MustCompile(`(\d+)x$`)

// encodeSnapshot renders a snapshot as JSON.
//
// Parameters:
//   - snap: the snapshot.
//
// Returns:
//   - string: the JSON document.
func encodeSnapshot(snap *snapshot) string {
	if snap.Bars == nil {
		snap.Bars = []barJSON{}
	}

	content, err := json.Marshal(snap)
	if err != nil {
		// Every field is a plain value, so encoding cannot fail.
		panic(err)
	}

	return string(content)
}

// barsJSON renders bars with their levels.
//
// Parameters:
//   - bars: the bars.
//   - warn: the warning threshold.
//   - crit: the critical threshold.
//
// Returns:
//   - []barJSON: the rendered bars.
func barsJSON(bars []usage.Bar, warn, crit float64) []barJSON {
	out := make([]barJSON, 0, len(bars))

	for _, bar := range bars {
		out = append(out, barJSON{
			ID:         bar.ID,
			Kind:       bar.Kind,
			Group:      bar.Group,
			Label:      bar.Label,
			Short:      bar.Short,
			Percent:    bar.Percent,
			ResetsAtMs: millis(bar.ResetsAt),
			Severity:   bar.Severity,
			Level:      usage.LevelOf(bar, warn, crit),
			Headline:   bar.Headline,
			Credit:     bar.Credit,
		})
	}

	return out
}

// extraToJSON renders extra usage.
//
// Parameters:
//   - extra: the extra usage, or nil.
//   - warn: the warning threshold.
//   - crit: the critical threshold.
//
// Returns:
//   - *extraJSON: the rendered section, or nil.
func extraToJSON(extra *usage.Extra, warn, crit float64) *extraJSON {
	if extra == nil {
		return nil
	}

	out := &extraJSON{
		Percent:      extra.Percent,
		Used:         "",
		Limit:        "",
		Level:        usage.LevelNormal,
		LimitReached: extra.LimitReached,
	}
	if extra.Used != nil {
		out.Used = extra.Used.String()
	}

	if extra.Limit != nil {
		out.Limit = extra.Limit.String()
	}

	if extra.Percent != nil {
		graded := usage.Bar{Percent: *extra.Percent} //nolint:exhaustruct_v5 // Only Percent is graded.

		out.Level = usage.LevelOf(graded, warn, crit)
	}

	if extra.LimitReached {
		out.Level = usage.LevelCritical
	}

	return out
}

// planName turns the subscription and rate limit tier into a label.
//
// Parameters:
//   - subscription: the plan, such as max.
//   - tier: the tier, such as default_claude_max_5x.
//
// Returns:
//   - string: a label such as "Max 5x", or empty.
func planName(subscription, tier string) string {
	plan := []rune(strings.ToLower(strings.TrimSpace(subscription)))
	if len(plan) == 0 {
		return ""
	}

	plan[0] = unicode.ToUpper(plan[0])

	name := string(plan)

	if match := tierMultiplier.FindStringSubmatch(tier); match != nil {
		name += " " + match[1] + "x"
	}

	return name
}

// millis converts a time to epoch milliseconds.
//
// Parameters:
//   - moment: the time, possibly zero.
//
// Returns:
//   - int64: milliseconds since the epoch, or 0 for the zero time.
func millis(moment time.Time) int64 {
	if moment.IsZero() {
		return 0
	}

	return moment.UnixMilli()
}
