// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNumberUnmarshalJSON covers every accepted shape of a numeric field.
//
// The API has sent numbers both bare and quoted, so both must decode. Values
// that are not finite numbers decode as unknown, so one odd field never fails
// the whole payload.
func TestNumberUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		value  float64
		wantOK bool
	}{
		{name: "integer", input: `6`, value: 6, wantOK: true},
		{name: "fraction", input: `12.5`, value: 12.5, wantOK: true},
		{name: "negative", input: `-3`, value: -3, wantOK: true},
		{name: "exponent", input: `1e2`, value: 100, wantOK: true},
		{name: "padded", input: " 7 ", value: 7, wantOK: true},
		{name: "quoted", input: `"42.25"`, value: 42.25, wantOK: true},
		{name: "quoted with spaces", input: `" 9 "`, value: 9, wantOK: true},
		{name: "null", input: `null`},
		{name: "quoted text", input: `"abc"`},
		{name: "empty string", input: `""`},
		{name: "quoted NaN", input: `"NaN"`},
		{name: "quoted infinity", input: `"Infinity"`},
		{name: "overflow", input: `1e400`},
		{name: "boolean", input: `true`},
		{name: "object", input: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := number{value: 99, ok: true}

			require.NoError(t, got.UnmarshalJSON([]byte(tt.input)))
			assert.Equal(t, tt.wantOK, got.ok)
			assert.InDelta(t, tt.value, got.value, 1e-9)
		})
	}
}

// TestNumberUnmarshalJSONMalformedString returns an error for a broken
// string literal.
//
// A string with an invalid escape is a malformed payload rather than an odd
// value, so it is reported instead of being read as unknown.
func TestNumberUnmarshalJSONMalformedString(t *testing.T) {
	t.Parallel()

	got := number{value: 5, ok: true}

	err := got.UnmarshalJSON([]byte(`"\q"`))
	require.Error(t, err)
	require.ErrorContains(t, err, "decode numeric string")
	assert.False(t, got.ok)
	assert.Zero(t, got.value)
}

// TestNumberInsideStruct checks decoding through encoding/json.
//
// encoding/json calls UnmarshalJSON for a present key only, so a missing key
// must leave the field unknown as well.
func TestNumberInsideStruct(t *testing.T) {
	t.Parallel()

	var win window

	require.NoError(t, json.Unmarshal([]byte(`{"utilization":"31"}`), &win))
	assert.True(t, win.Utilization.ok)
	assert.InDelta(t, 31.0, win.Utilization.value, 1e-9)

	var empty window

	require.NoError(t, json.Unmarshal([]byte(`{}`), &empty))
	assert.False(t, empty.Utilization.ok)
}

// TestNamedName prefers the display name and falls back to the ID.
func TestNamedName(t *testing.T) {
	t.Parallel()

	id := "claude-fable"
	emptyID := ""

	tests := []struct {
		target *named
		name   string
		want   string
	}{
		{name: "nil target", target: nil, want: ""},
		{name: "display name", target: &named{ID: &id, DisplayName: "Fable"}, want: "Fable"},
		{name: "id fallback", target: &named{ID: &id}, want: "claude-fable"},
		{name: "empty id", target: &named{ID: &emptyID}, want: ""},
		{name: "nothing", target: &named{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.target.name())
		})
	}
}

// TestParseTime accepts RFC 3339 with fractional seconds and returns zero
// for anything else.
func TestParseTime(t *testing.T) {
	t.Parallel()

	valid := "2026-10-07T03:19:59.825797+00:00"
	whole := "2026-10-13T15:00:00Z"
	empty := ""
	malformed := "next tuesday"

	assert.True(t, parseTime(nil).IsZero())
	assert.True(t, parseTime(&empty).IsZero())
	assert.True(t, parseTime(&malformed).IsZero())

	want := time.Date(2026, time.October, 7, 3, 19, 59, 825797000, time.UTC)
	assert.True(t, want.Equal(parseTime(&valid)), "got %v", parseTime(&valid))

	wantWhole := time.Date(2026, time.October, 13, 15, 0, 0, 0, time.UTC)
	assert.True(t, wantWhole.Equal(parseTime(&whole)), "got %v", parseTime(&whole))
}

// TestSurfaceName reads a surface given as a string or as a named object.
func TestSurfaceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "absent", raw: "", want: ""},
		{name: "null", raw: `null`, want: ""},
		{name: "string", raw: `"Claude Code"`, want: "Claude Code"},
		{name: "object with display name", raw: `{"id":"cc","display_name":"Claude Code"}`, want: "Claude Code"},
		{name: "object with id only", raw: `{"id":"cowork"}`, want: "cowork"},
		{name: "number", raw: `12`, want: ""},
		{name: "array", raw: `["a"]`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, surfaceName(json.RawMessage(tt.raw)))
		})
	}
}
