// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
)

// stateTime is the base time for state fixtures.
var stateTime = time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

// sampleState returns a state with every field set.
//
// Returns:
//   - State: the populated state.
func sampleState() State {
	return State{
		LastAttempt: stateTime,
		Data: &Data{
			FetchedAt:   stateTime.Add(-time.Minute),
			Source:      "api",
			AccountUUID: "0b5e7a10-1111-4111-8111-111111111111",
			Payload:     json.RawMessage(`{"five_hour":{"utilization":42}}`),
		},
		AuthBlock: &schedule.AuthBlock{
			TokenExpiresAt: stateTime.Add(time.Hour),
			Since:          stateTime.Add(-time.Hour),
			Reason:         "unauthorized",
		},
		Backoff: schedule.Backoff{
			Until:       stateTime.Add(5 * time.Minute),
			Since:       stateTime.Add(-10 * time.Minute),
			Kind:        schedule.BackoffRateLimited,
			Consecutive: 3,
		},
		Alerts: notify.Ledger{
			ReplaceIDs: map[string]uint32{"session": 7},
			Auth:       "unauthorized",
			Entries: []notify.Entry{{
				ResetsAt:  stateTime.Add(2 * time.Hour),
				At:        stateTime,
				Bar:       "session",
				Threshold: 80,
			}},
		},
		Version: Version,
	}
}

// assertSameState fails the test unless want and got hold the same state.
//
// The File store indents the raw payload, so payloads are compared as JSON
// and every other field exactly.
//
// Parameters:
//   - t: test handle.
//   - want: the expected state.
//   - got: the actual state.
func assertSameState(t *testing.T, want, got State) {
	t.Helper()

	if want.Data == nil || got.Data == nil {
		assert.Equal(t, want, got)

		return
	}

	assert.JSONEq(t, string(want.Data.Payload), string(got.Data.Payload))

	wantData, gotData := *want.Data, *got.Data
	wantData.Payload, gotData.Payload = nil, nil
	want.Data, got.Data = &wantData, &gotData

	assert.Equal(t, want, got)
}

// TestFresh returns an empty state in the current format.
func TestFresh(t *testing.T) {
	t.Parallel()

	fresh := Fresh()

	assert.Equal(t, Version, fresh.Version)
	assert.Equal(t, State{Version: Version}, fresh)
}

// TestStateJSONOmitsEmptyFields keeps a fresh state file down to its version.
func TestStateJSONOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(Fresh())
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1}`, string(encoded))
}

// TestCloneDeepCopies returns an equal state that shares no memory with the
// original.
func TestCloneDeepCopies(t *testing.T) {
	t.Parallel()

	original := sampleState()

	copied, err := clone(original)
	require.NoError(t, err)
	assert.Equal(t, original, copied)

	original.Data.Payload[2] = 'X'
	original.Data.Source = "claude-code"
	original.AuthBlock.Reason = "scope"
	original.Alerts.ReplaceIDs["session"] = 99
	original.Alerts.Entries[0].Threshold = 95

	assert.Equal(t, sampleState(), copied)
}

// TestCloneEncodeError reports a payload that is not valid JSON.
func TestCloneEncodeError(t *testing.T) {
	t.Parallel()

	broken := Fresh()
	broken.Data = &Data{Payload: json.RawMessage(`{`)}

	_, err := clone(broken)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copy state")
}
