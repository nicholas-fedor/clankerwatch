// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveFixture is the anonymized payload captured from the usage endpoint.
const liveFixture = "live.json"

// readFixture returns a file from testdata.
//
// Parameters:
//   - t: test handle.
//   - name: file name inside testdata.
//
// Returns:
//   - []byte: the file contents.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	return data
}

// barIDs returns the IDs of bars in order.
//
// Parameters:
//   - bars: the bars.
//
// Returns:
//   - []string: one ID per bar.
func barIDs(bars []Bar) []string {
	ids := make([]string, 0, len(bars))

	for _, bar := range bars {
		ids = append(ids, bar.ID)
	}

	return ids
}

// TestParseLiveFixture checks every field of the captured payload.
//
// The fixture is what the endpoint really sends, so a regression here means
// the panel would show wrong numbers to every user.
func TestParseLiveFixture(t *testing.T) {
	t.Parallel()

	got, err := Parse(readFixture(t, liveFixture))
	require.NoError(t, err)
	require.Len(t, got.Bars, 3)

	assert.Nil(t, got.Extra, "extra usage is disabled in the fixture")

	session := got.Bars[0]
	assert.Equal(t, "session", session.ID)
	assert.Equal(t, "session", session.Kind)
	assert.Equal(t, "session", session.Group)
	assert.Equal(t, "Current session", session.Label)
	assert.Equal(t, "5h", session.Short)
	assert.Equal(t, "normal", session.Severity)
	assert.InDelta(t, 6.0, session.Percent, 1e-9)
	assert.False(t, session.Headline)
	assert.False(t, session.Credit)
	assert.True(t, time.Date(2026, time.October, 7, 3, 19, 59, 825797000, time.UTC).Equal(session.ResetsAt))

	weekly := got.Bars[1]
	assert.Equal(t, "weekly_all", weekly.ID)
	assert.Equal(t, "weekly", weekly.Group)
	assert.Equal(t, "Current week (all models)", weekly.Label)
	assert.Equal(t, "7d", weekly.Short)
	assert.InDelta(t, 18.0, weekly.Percent, 1e-9)
	assert.True(t, weekly.Headline)

	fable := got.Bars[2]
	assert.Equal(t, "weekly_scoped:model:fable", fable.ID)
	assert.Equal(t, "weekly_scoped", fable.Kind)
	assert.Equal(t, "Current week (Fable)", fable.Label)
	assert.Equal(t, "Fable", fable.Short)
	assert.InDelta(t, 0.0, fable.Percent, 1e-9)
	assert.True(t, time.Date(2026, time.October, 13, 15, 0, 0, 0, time.UTC).Equal(fable.ResetsAt))
}

// TestParseErrors covers payloads that cannot be used.
func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target error
		name   string
		input  string
	}{
		{name: "not JSON", input: `{`},
		{name: "array", input: `[]`},
		{name: "string", input: `"usage"`},
		{name: "wrong type for limits", input: `{"limits":"many"}`},
		{name: "wrong type for a window", input: `{"five_hour":[1]}`},
		{name: "malformed numeric string", input: `{"five_hour":{"utilization":"\q"}}`},
		{name: "null", input: `null`, target: ErrUnrecognized},
		{name: "empty object", input: `{}`, target: ErrUnrecognized},
		{name: "unknown keys only", input: `{"spend":{},"tangelo":null}`, target: ErrUnrecognized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse([]byte(tt.input))
			require.Error(t, err)

			if tt.target != nil {
				require.ErrorIs(t, err, tt.target)
			} else {
				require.NotErrorIs(t, err, ErrUnrecognized)
				require.ErrorContains(t, err, "decode usage payload")
			}

			assert.Empty(t, got.Bars)
			assert.Nil(t, got.Extra)
		})
	}
}

// TestParseKnownKeyWithNull accepts a payload whose only known key is null.
//
// Claude Code trusts a payload by its keys, not their values, so a null key
// yields an empty but valid result rather than an error.
func TestParseKnownKeyWithNull(t *testing.T) {
	t.Parallel()

	for _, key := range knownKeys {
		got, err := Parse([]byte(`{"` + key + `":null}`))
		require.NoError(t, err, "key %s", key)
		assert.Empty(t, got.Bars, "key %s", key)
		assert.Nil(t, got.Extra, "key %s", key)
	}
}

// TestParseFlatWindowsOnly builds bars from the older flat windows.
//
// The highest window becomes the headline, so the single-value indicator
// shows the limit closest to running out.
func TestParseFlatWindowsOnly(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{
		"five_hour": {"utilization": 12, "resets_at": "2026-10-07T03:00:00Z"},
		"seven_day": {"utilization": "40"},
		"seven_day_sonnet": {"utilization": 75.5},
		"seven_day_opus": {"utilization": 3},
		"limits": []
	}`))
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"session", "weekly_all", "weekly_scoped:model:sonnet", "weekly_scoped:model:opus"},
		barIDs(got.Bars))

	headlines := 0

	for _, bar := range got.Bars {
		if bar.Headline {
			headlines++

			assert.Equal(t, "weekly_scoped:model:sonnet", bar.ID)
		}
	}

	assert.Equal(t, 1, headlines)
	assert.Equal(t, "Current week (Sonnet only)", got.Bars[2].Label)
	assert.Equal(t, "Opus", got.Bars[3].Short)
	assert.False(t, got.Bars[0].ResetsAt.IsZero())
	assert.True(t, got.Bars[1].ResetsAt.IsZero())
}

// TestParseFlatWindowWithoutUtilization skips a window whose value is unknown.
func TestParseFlatWindowWithoutUtilization(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{"five_hour":{"utilization":null},"seven_day":{"utilization":5}}`))
	require.NoError(t, err)
	require.Len(t, got.Bars, 1)
	assert.Equal(t, "weekly_all", got.Bars[0].ID)
	assert.True(t, got.Bars[0].Headline)
}

// TestParseMergesMissingFlatBars puts flat session and weekly bars in front
// when the limits list leaves them out.
func TestParseMergesMissingFlatBars(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{
		"five_hour": {"utilization": 20},
		"seven_day": {"utilization": 30},
		"limits": [
			{"kind": "weekly_scoped", "percent": 50, "scope": {"surface": "Claude Code"}}
		]
	}`))
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"session", "weekly_all", "weekly_scoped:surface:claude-code"},
		barIDs(got.Bars))
	assert.Equal(t, "Current week (Claude Code)", got.Bars[2].Label)
}

// TestParseLimitsWinOverFlat keeps the limits list authoritative.
//
// When the list has its own session bar, the flat window must not add a
// second one with possibly stale numbers.
func TestParseLimitsWinOverFlat(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{
		"five_hour": {"utilization": 99},
		"limits": [{"kind": "session", "percent": 10, "severity": "WARNING", "is_active": true}]
	}`))
	require.NoError(t, err)
	require.Len(t, got.Bars, 1)
	assert.InDelta(t, 10.0, got.Bars[0].Percent, 1e-9)
	assert.Equal(t, "warning", got.Bars[0].Severity, "severity is lowercased")
	assert.True(t, got.Bars[0].Headline)
}

// TestParseSkipsRowsWithoutKind ignores limits rows that name no kind.
func TestParseSkipsRowsWithoutKind(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{"limits":[{"percent":5},{"kind":"session","percent":7}]}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"session"}, barIDs(got.Bars))
}

// TestParseCreditBar appends the credit window last and marks it.
func TestParseCreditBar(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{
		"limits": [{"kind": "session", "percent": 1}],
		"cinder_cove": {"utilization": 64, "resets_at": "2026-11-01T00:00:00Z"}
	}`))
	require.NoError(t, err)
	require.Len(t, got.Bars, 2)

	credit := got.Bars[1]
	assert.Equal(t, "credit", credit.ID)
	assert.True(t, credit.Credit)
	assert.Equal(t, "Claude Code and Cowork credit", credit.Label)
	assert.Equal(t, "Credit", credit.Short)
	assert.InDelta(t, 64.0, credit.Percent, 1e-9)
}

// TestParseDuplicateIDs keeps repeated bars addressable.
func TestParseDuplicateIDs(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{"limits":[
		{"kind":"weekly_scoped","scope":{"model":{"display_name":"Fable"}}},
		{"kind":"weekly_scoped","scope":{"model":{"display_name":"fable"}}},
		{"kind":"weekly_scoped","scope":{"model":{"display_name":"FABLE"}}}
	]}`))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"weekly_scoped:model:fable",
		"weekly_scoped:model:fable#2",
		"weekly_scoped:model:fable#3",
	}, barIDs(got.Bars))
}

// TestParseSuffixedKindStaysUnique checks suffixed IDs stay unique when the
// server already sends an ID that looks suffixed.
//
// The server's kind becomes the ID of an unknown limit verbatim, so a kind
// such as "monthly#2" can sit next to two "monthly" rows. Two bars with one
// ID could not be told apart by the panel or by alert state.
func TestParseSuffixedKindStaysUnique(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{"limits":[{"kind":"monthly#2"},{"kind":"monthly"},{"kind":"monthly"}]}`))
	require.NoError(t, err)

	ids := barIDs(got.Bars)
	seen := make(map[string]bool, len(ids))

	for _, id := range ids {
		assert.False(t, seen[id], "duplicate ID %q in %v", id, ids)
		seen[id] = true
	}
}

// TestParseExtraUsage covers the pay-as-you-go section.
func TestParseExtraUsage(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{"extra_usage":{
		"is_enabled": true,
		"monthly_limit": 5000,
		"used_credits": "1250",
		"utilization": null,
		"currency": "EUR",
		"decimal_places": 2,
		"disabled_reason": "none"
	}}`))
	require.NoError(t, err)
	require.NotNil(t, got.Extra)

	extra := got.Extra
	require.NotNil(t, extra.Used)
	require.NotNil(t, extra.Limit)
	require.NotNil(t, extra.Percent)
	assert.Equal(t, "€12.50", extra.Used.String())
	assert.Equal(t, "€50.00", extra.Limit.String())
	assert.InDelta(t, 25.0, *extra.Percent, 1e-9, "derived from the amounts")
	assert.Equal(t, "none", extra.DisabledReason)
	assert.False(t, extra.LimitReached)
}

// TestParseExtraUsageLimitReached reports a reached limit even when disabled.
//
// The server disables extra usage once the limit is hit, and that is exactly
// the state a user needs to see.
func TestParseExtraUsageLimitReached(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{"extra_usage":{
		"is_enabled": false,
		"spend_limit_reached": true,
		"used_credits": 10000,
		"monthly_limit": 10000,
		"utilization": 100
	}}`))
	require.NoError(t, err)
	require.NotNil(t, got.Extra)

	assert.True(t, got.Extra.LimitReached)
	require.NotNil(t, got.Extra.Used)
	assert.Equal(t, 2, got.Extra.Used.Decimals, "decimals default to two")
	assert.Empty(t, got.Extra.Used.Currency)
	require.NotNil(t, got.Extra.Percent)
	assert.InDelta(t, 100.0, *got.Extra.Percent, 1e-9, "the server's utilization wins")
}

// TestParseExtraUsageDisabled hides a disabled section.
func TestParseExtraUsageDisabled(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte(`{"extra_usage":{"is_enabled":false,"used_credits":5}}`))
	require.NoError(t, err)
	assert.Nil(t, got.Extra)
}

// TestExtraFromNil returns nil for a missing section.
func TestExtraFromNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, extraFrom(nil))
}

// TestMoney covers the conversion of a raw amount to minor units.
func TestMoney(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want   *Money
		name   string
		amount number
	}{
		{name: "unknown", amount: number{value: 5, ok: false}},
		{name: "rounds half away from zero", amount: number{value: 2.5, ok: true}, want: &Money{Minor: 3}},
		{name: "negative", amount: number{value: -1.4, ok: true}, want: &Money{Minor: -1}},
		{name: "too large", amount: number{value: 9.3e18, ok: true}},
		{name: "exactly two to the 63", amount: number{value: 9223372036854775808, ok: true}},
		{name: "too small", amount: number{value: -9.3e18, ok: true}},
		{
			name:   "most negative",
			amount: number{value: -9223372036854775808, ok: true},
			want:   &Money{Minor: -9223372036854775808},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := money(tt.amount, 2, "USD")
			if tt.want == nil {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, tt.want.Minor, got.Minor)
			assert.Equal(t, 2, got.Decimals)
			assert.Equal(t, "USD", got.Currency)
		})
	}
}

// TestExtraPercent prefers the server's value and derives one otherwise.
func TestExtraPercent(t *testing.T) {
	t.Parallel()

	used := &Money{Minor: 300}
	limit := &Money{Minor: 1200}
	zero := &Money{Minor: 0}

	got := extraPercent(number{value: 7, ok: true}, used, limit)
	require.NotNil(t, got)
	assert.InDelta(t, 7.0, *got, 1e-9)

	got = extraPercent(number{}, used, limit)
	require.NotNil(t, got)
	assert.InDelta(t, 25.0, *got, 1e-9)

	assert.Nil(t, extraPercent(number{}, used, zero), "a zero limit cannot give a percentage")
	assert.Nil(t, extraPercent(number{}, nil, limit))
	assert.Nil(t, extraPercent(number{}, used, nil))
}

// TestLabels covers the ID and label scheme for every kind of row.
func TestLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		kind      string
		scopeKind string
		scopeName string
		wantID    string
		wantLabel string
		wantShort string
	}{
		{
			name: "session", kind: "session",
			wantID: "session", wantLabel: "Current session", wantShort: "5h",
		},
		{
			name: "weekly all", kind: "weekly_all",
			wantID: "weekly_all", wantLabel: "Current week (all models)", wantShort: "7d",
		},
		{
			name: "weekly scoped without scope", kind: "weekly_scoped",
			wantID: "weekly_scoped", wantLabel: "Current week (scoped)", wantShort: "7d",
		},
		{
			name: "weekly scoped to a model", kind: "weekly_scoped", scopeKind: "model", scopeName: "Fable 2",
			wantID: "weekly_scoped:model:fable-2", wantLabel: "Current week (Fable 2)", wantShort: "Fable 2",
		},
		{
			name: "unknown kind", kind: "monthly_all",
			wantID: "monthly_all", wantLabel: "Monthly all", wantShort: "Monthly all",
		},
		{
			name: "unknown kind with scope", kind: "daily", scopeKind: "surface", scopeName: "Cowork",
			wantID: "daily:cowork", wantLabel: "Daily (Cowork)", wantShort: "Daily",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id, label, short := labels(tt.kind, tt.scopeKind, tt.scopeName)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantLabel, label)
			assert.Equal(t, tt.wantShort, short)
		})
	}
}

// TestRowScope prefers a model over a surface.
func TestRowScope(t *testing.T) {
	t.Parallel()

	kind, name := rowScope(nil)
	assert.Empty(t, kind)
	assert.Empty(t, name)

	kind, name = rowScope(&limitScope{Model: &named{DisplayName: "Fable"}, Surface: []byte(`"Cowork"`)})
	assert.Equal(t, scopeModel, kind)
	assert.Equal(t, "Fable", name)

	kind, name = rowScope(&limitScope{Surface: []byte(`"Cowork"`)})
	assert.Equal(t, scopeSurface, kind)
	assert.Equal(t, "Cowork", name)

	kind, name = rowScope(&limitScope{Model: &named{}, Surface: []byte(`null`)})
	assert.Empty(t, kind)
	assert.Empty(t, name)
}

// TestSlug covers lowercasing and hyphen joining.
func TestSlug(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                "",
		"Fable":           "fable",
		"Claude Code":     "claude-code",
		"  Leading":       "leading",
		"Trailing!!":      "trailing",
		"a -- b":          "a-b",
		"Opus 4.1":        "opus-4-1",
		"Ünïcödé Mödel":   "ünïcödé-mödel",
		"!!!":             "",
		"snake_case_name": "snake-case-name",
	}

	for input, want := range tests {
		assert.Equal(t, want, slug(input), "slug(%q)", input)
	}
}

// TestHumanize turns a server kind into a label.
func TestHumanize(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":            "",
		"_":           "",
		"monthly_all": "Monthly all",
		"daily":       "Daily",
		"_leading":    "Leading",
		"éclair":      "Éclair",
	}

	for input, want := range tests {
		assert.Equal(t, want, humanize(input), "humanize(%q)", input)
	}
}

// TestHasKnownKey matches each key Claude Code requires.
func TestHasKnownKey(t *testing.T) {
	t.Parallel()

	assert.False(t, hasKnownKey(nil))
	assert.False(t, hasKnownKey(map[string]json.RawMessage{"other": nil}))

	for _, key := range knownKeys {
		assert.True(t, hasKnownKey(map[string]json.RawMessage{key: nil}), "key %s", key)
	}
}

// TestCreditBarMissing reports no bar for an unusable window.
func TestCreditBarMissing(t *testing.T) {
	t.Parallel()

	_, ok := creditBar(nil)
	assert.False(t, ok)

	bar, ok := creditBar(&window{Utilization: number{}})
	assert.False(t, ok)
	assert.False(t, bar.Credit)
}
