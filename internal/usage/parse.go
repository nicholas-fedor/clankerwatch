// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Server kinds and the labels Claude Code shows for them.
const (
	kindSession      = "session"
	kindWeeklyAll    = "weekly_all"
	kindWeeklyScoped = "weekly_scoped"
	kindCredit       = "credit"
	groupWeekly      = "weekly"

	labelSession = "Current session"
	labelWeekly  = "Current week (all models)"
	shortSession = "5h"
	shortWeekly  = "7d"

	scopeModel   = "model"
	scopeSurface = "surface"

	// defaultDecimals is the minor-unit digits assumed when the server omits them.
	defaultDecimals = 2

	// percentScale converts a fraction to a percentage.
	percentScale = 100
)

// ErrUnrecognized indicates a payload that has none of the known usage keys.
var ErrUnrecognized = errors.New("usage payload not recognized")

// knownKeys mirrors the keys Claude Code requires before it trusts a payload.
var knownKeys = []string{
	"five_hour", "seven_day", "seven_day_oauth_apps", "seven_day_opus",
	"seven_day_sonnet", "cinder_cove", "extra_usage", "limits",
}

// Parse turns a usage payload into bars in the order Claude Code shows them.
//
// The server's limits list is authoritative. The older flat windows fill in
// when the list is missing or empty, and supply the session and weekly bars
// when the list leaves them out.
//
// Parameters:
//   - data: the raw payload.
//
// Returns:
//   - Usage: the bars and extra usage.
//   - error: ErrUnrecognized for a payload without known keys, or a decode error.
func Parse(data []byte) (Usage, error) {
	var keys map[string]json.RawMessage

	err := json.Unmarshal(data, &keys)
	if err != nil {
		return Usage{Bars: nil, Extra: nil}, fmt.Errorf("decode usage payload: %w", err)
	}

	if !hasKnownKey(keys) {
		return Usage{Bars: nil, Extra: nil}, ErrUnrecognized
	}

	var body payload

	err = json.Unmarshal(data, &body)
	if err != nil {
		return Usage{Bars: nil, Extra: nil}, fmt.Errorf("decode usage payload: %w", err)
	}

	bars := limitBars(body.Limits)
	if len(bars) == 0 {
		bars = flatBars(body)
	} else {
		bars = mergeFlat(bars, body)
	}

	if credit, ok := creditBar(body.CinderCove); ok {
		bars = append(bars, credit)
	}

	return Usage{Bars: uniqueIDs(bars), Extra: extraFrom(body.ExtraUsage)}, nil
}

// hasKnownKey reports whether the payload has any key Claude Code requires.
//
// Parameters:
//   - keys: the payload's top-level keys.
//
// Returns:
//   - bool: true when a known key is present, even with a null value.
func hasKnownKey(keys map[string]json.RawMessage) bool {
	for _, key := range knownKeys {
		if _, ok := keys[key]; ok {
			return true
		}
	}

	return false
}

// limitBars converts the server's limits list.
//
// Parameters:
//   - rows: the limits list.
//
// Returns:
//   - []Bar: one bar per row with a kind.
func limitBars(rows []limitRow) []Bar {
	bars := make([]Bar, 0, len(rows))

	for _, row := range rows {
		if row.Kind != "" {
			bars = append(bars, barFromLimit(row))
		}
	}

	return bars
}

// barFromLimit converts one limits row.
//
// Parameters:
//   - row: the row.
//
// Returns:
//   - Bar: the bar with Claude Code's labels.
func barFromLimit(row limitRow) Bar {
	scopeKind, scopeName := rowScope(row.Scope)
	id, label, short := labels(row.Kind, scopeKind, scopeName)

	return Bar{
		ID:       id,
		Kind:     row.Kind,
		Group:    row.Group,
		Label:    label,
		Short:    short,
		Percent:  row.Percent.value,
		ResetsAt: parseTime(row.ResetsAt),
		Severity: strings.ToLower(row.Severity),
		Headline: row.IsActive,
		Credit:   false,
	}
}

// rowScope returns what a row is scoped to.
//
// Parameters:
//   - scope: the row's scope, or nil.
//
// Returns:
//   - kind: model, surface, or empty.
//   - name: the model or surface name.
//
//nolint:nonamedreturns // Same-type returns need names.
func rowScope(scope *limitScope) (kind, name string) {
	if scope == nil {
		return "", ""
	}

	name = scope.Model.name()
	if name != "" {
		return scopeModel, name
	}

	name = surfaceName(scope.Surface)
	if name != "" {
		return scopeSurface, name
	}

	return "", ""
}

// labels returns the ID and labels Claude Code uses for a row.
//
// Parameters:
//   - kind: the server kind.
//   - scopeKind: model, surface, or empty.
//   - scopeName: the scope's name.
//
// Returns:
//   - id: the stable bar ID.
//   - label: the long label.
//   - short: the panel label.
//
//nolint:nonamedreturns // Same-type returns need names.
func labels(kind, scopeKind, scopeName string) (id, label, short string) {
	switch kind {
	case kindSession:
		return kindSession, labelSession, shortSession
	case kindWeeklyAll:
		return kindWeeklyAll, labelWeekly, shortWeekly
	case kindWeeklyScoped:
		if scopeName == "" {
			return kindWeeklyScoped, "Current week (scoped)", shortWeekly
		}

		id = kindWeeklyScoped + ":" + scopeKind + ":" + slug(scopeName)

		return id, "Current week (" + scopeName + ")", scopeName
	default:
		label = humanize(kind)
		if scopeName == "" {
			return kind, label, label
		}

		return kind + ":" + slug(scopeName), label + " (" + scopeName + ")", label
	}
}

// flatWindow converts a flat window to a bar.
//
// Parameters:
//   - win: the window, or nil.
//   - template: the bar's identity and labels.
//
// Returns:
//   - Bar: the bar.
//   - bool: false when the window is missing or has no utilization.
func flatWindow(win *window, template Bar) (Bar, bool) {
	if win == nil || !win.Utilization.ok {
		return Bar{}, false
	}

	template.Percent = win.Utilization.value
	template.ResetsAt = parseTime(win.ResetsAt)

	return template, true
}

// flatTemplate builds the identity and labels of a flat-window bar.
//
// Parameters:
//   - id: the bar ID.
//   - kind: the server kind.
//   - group: the server group.
//   - label: the long label.
//   - short: the panel label.
//
// Returns:
//   - Bar: a bar with no usage values.
func flatTemplate(id, kind, group, label, short string) Bar {
	return Bar{
		ID: id, Kind: kind, Group: group, Label: label, Short: short,
		Percent: 0, ResetsAt: parseTime(nil), Severity: "", Headline: false, Credit: false,
	}
}

// sessionBar returns the session bar from the flat five_hour window.
//
// Parameters:
//   - body: the payload.
//
// Returns:
//   - Bar: the session bar.
//   - bool: false when the window is unusable.
func sessionBar(body payload) (Bar, bool) {
	return flatWindow(body.FiveHour, flatTemplate(kindSession, kindSession, kindSession, labelSession, shortSession))
}

// weeklyBar returns the weekly bar from the flat seven_day window.
//
// Parameters:
//   - body: the payload.
//
// Returns:
//   - Bar: the weekly bar.
//   - bool: false when the window is unusable.
func weeklyBar(body payload) (Bar, bool) {
	return flatWindow(body.SevenDay, flatTemplate(kindWeeklyAll, kindWeeklyAll, groupWeekly, labelWeekly, shortWeekly))
}

// flatBars builds bars from the flat windows when the limits list is empty.
//
// The highest bar becomes the headline.
//
// Parameters:
//   - body: the payload.
//
// Returns:
//   - []Bar: the usable windows in display order.
func flatBars(body payload) []Bar {
	var bars []Bar

	add := func(bar Bar, ok bool) {
		if ok {
			bars = append(bars, bar)
		}
	}

	add(sessionBar(body))
	add(weeklyBar(body))
	add(flatWindow(body.SevenDaySonnet, flatTemplate(
		"weekly_scoped:model:sonnet", kindWeeklyScoped, groupWeekly, "Current week (Sonnet only)", "Sonnet",
	)))
	add(flatWindow(body.SevenDayOpus, flatTemplate(
		"weekly_scoped:model:opus", kindWeeklyScoped, groupWeekly, "Current week (Opus)", "Opus",
	)))

	if len(bars) > 0 {
		top := 0

		for index, bar := range bars {
			if bar.Percent > bars[top].Percent {
				top = index
			}
		}

		bars[top].Headline = true
	}

	return bars
}

// mergeFlat adds the session and weekly bars from the flat windows when the
// limits list leaves them out, keeping them in front as /usage does.
//
// Parameters:
//   - bars: bars from the limits list.
//   - body: the payload.
//
// Returns:
//   - []Bar: the bars with any missing session or weekly bar in front.
func mergeFlat(bars []Bar, body payload) []Bar {
	has := func(kind string) bool {
		for _, bar := range bars {
			if bar.Kind == kind {
				return true
			}
		}

		return false
	}

	var front []Bar

	if !has(kindSession) {
		if bar, ok := sessionBar(body); ok {
			front = append(front, bar)
		}
	}

	if !has(kindWeeklyAll) {
		if bar, ok := weeklyBar(body); ok {
			front = append(front, bar)
		}
	}

	return append(front, bars...)
}

// creditBar converts the credit window.
//
// Parameters:
//   - win: the cinder_cove window, or nil.
//
// Returns:
//   - Bar: the credit bar.
//   - bool: false when the window is unusable.
func creditBar(win *window) (Bar, bool) {
	template := flatTemplate(kindCredit, kindCredit, kindCredit, "Claude Code and Cowork credit", "Credit")
	bar, ok := flatWindow(win, template)

	bar.Credit = ok

	return bar, ok
}

// extraFrom converts the pay-as-you-go section.
//
// Parameters:
//   - extra: the section, or nil.
//
// Returns:
//   - *Extra: the extra usage, or nil unless it is enabled or its limit is reached.
func extraFrom(extra *extraUsage) *Extra {
	if extra == nil || (!extra.IsEnabled && !extra.SpendLimitReached) {
		return nil
	}

	decimals := defaultDecimals
	if extra.DecimalPlaces != nil {
		decimals = *extra.DecimalPlaces
	}

	currency := ""
	if extra.Currency != nil {
		currency = *extra.Currency
	}

	result := &Extra{
		Percent:        nil,
		Used:           money(extra.UsedCredits, decimals, currency),
		Limit:          money(extra.MonthlyLimit, decimals, currency),
		LimitReached:   extra.SpendLimitReached,
		DisabledReason: "",
	}
	if extra.DisabledReason != nil {
		result.DisabledReason = *extra.DisabledReason
	}

	result.Percent = extraPercent(extra.Utilization, result.Used, result.Limit)

	return result
}

// money converts an amount in minor units.
//
// Parameters:
//   - amount: the amount.
//   - decimals: minor-unit digits.
//   - currency: the ISO 4217 code.
//
// Returns:
//   - *Money: the amount, or nil when unknown.
func money(amount number, decimals int, currency string) *Money {
	if !amount.ok || amount.value >= math.MaxInt64 || amount.value < math.MinInt64 {
		return nil
	}

	return &Money{Minor: int64(math.Round(amount.value)), Decimals: decimals, Currency: currency}
}

// extraPercent returns the server's utilization, or derives it from the
// amounts.
//
// Parameters:
//   - utilization: the server's percentage.
//   - used: the amount spent.
//   - limit: the monthly limit.
//
// Returns:
//   - *float64: the percentage, or nil when unknown.
func extraPercent(utilization number, used, limit *Money) *float64 {
	if utilization.ok {
		return new(utilization.value)
	}

	if used != nil && limit != nil && limit.Minor > 0 {
		return new(float64(used.Minor) / float64(limit.Minor) * percentScale)
	}

	return nil
}

// uniqueIDs suffixes repeated IDs so every bar stays addressable.
//
// A suffix skips any ID already taken, because the server's kind becomes the
// ID of an unknown limit and can itself look suffixed.
//
// Parameters:
//   - bars: the bars, modified in place.
//
// Returns:
//   - []Bar: the same bars with unique IDs.
func uniqueIDs(bars []Bar) []Bar {
	taken := make(map[string]bool, len(bars))

	for index := range bars {
		id := bars[index].ID

		for count := 2; taken[id]; count++ {
			id = bars[index].ID + "#" + strconv.Itoa(count)
		}

		bars[index].ID = id
		taken[id] = true
	}

	return bars
}

// slug lowercases a name and joins its words with hyphens.
//
// Parameters:
//   - name: a display name.
//
// Returns:
//   - string: the slug, such as claude-code.
func slug(name string) string {
	runes := make([]rune, 0, len(name))
	dash := false

	for _, char := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			runes = append(runes, char)
			dash = false
		case !dash && len(runes) > 0:
			runes = append(runes, '-')
			dash = true
		default:
		}
	}

	return strings.TrimSuffix(string(runes), "-")
}

// humanize turns a server kind into a label.
//
// Parameters:
//   - kind: a kind such as monthly_all.
//
// Returns:
//   - string: a label such as "Monthly all".
func humanize(kind string) string {
	text := strings.TrimSpace(strings.ReplaceAll(kind, "_", " "))
	if text == "" {
		return text
	}

	runes := []rune(text)

	runes[0] = unicode.ToUpper(runes[0])

	return string(runes)
}
