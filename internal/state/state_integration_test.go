// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package state_test exercises the state stores through public APIs, the way
// the daemon uses them across a restart.
package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// store is the interface the daemon uses for both stores.
type store interface {
	Load() (state.State, error)
	Save(current state.State) error
}

// restartTime is the base time for the restart flows.
var restartTime = time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

// runDaemonSession loads the state, records one fetch, one backoff, and one
// delivered alert, and saves it, as one daemon run would.
//
// Parameters:
//   - t: test handle.
//   - s: the store.
//   - now: the time of the run.
//
// Returns:
//   - state.State: the state that was saved.
func runDaemonSession(t *testing.T, s store, now time.Time) state.State {
	t.Helper()

	current, err := s.Load()
	require.NoError(t, err)

	current.LastAttempt = now
	current.Data = &state.Data{
		FetchedAt:   now,
		Source:      "api",
		AccountUUID: "0b5e7a10-1111-4111-8111-111111111111",
		Payload:     json.RawMessage(`{"five_hour":{"utilization":85}}`),
	}
	current.Backoff = schedule.Backoff{Until: now.Add(time.Minute), Since: now, Kind: schedule.BackoffServer, Consecutive: 1}

	bar := usage.Bar{ID: "session", Label: "Current session", Percent: 85, ResetsAt: now.Add(3 * time.Hour)}
	for _, alert := range current.Alerts.Evaluate(now, []usage.Bar{bar}, []float64{80, 95}) {
		current.Alerts.Record(alert, 11, now)
	}

	require.NoError(t, s.Save(current))

	return current
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
func assertSameState(t *testing.T, want, got state.State) {
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

// TestFileSurvivesRestart saves through one File and loads through another on
// the same path, and the restored ledger still suppresses the alert.
func TestFileSurvivesRestart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "clankerwatch", "state.json")

	saved := runDaemonSession(t, state.File{Path: path}, restartTime)
	require.Len(t, saved.Alerts.Entries, 1)

	restarted := state.File{Path: path}

	loaded, err := restarted.Load()
	require.NoError(t, err)
	assertSameState(t, saved, loaded)
	assert.Equal(t, uint32(11), loaded.Alerts.ReplaceIDs["session"])

	bar := usage.Bar{ID: "session", Percent: 90, ResetsAt: restartTime.Add(3*time.Hour + time.Minute)}
	assert.Empty(t, loaded.Alerts.Evaluate(restartTime.Add(time.Minute), []usage.Bar{bar}, []float64{80, 95}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestFileDiscardsForeignFormat starts fresh over a file from another format
// and then overwrites it on the next save.
func TestFileDiscardsForeignFormat(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":99,"data":{"source":"api"}}`), 0o600))

	file := state.File{Path: path}

	loaded, err := file.Load()
	require.ErrorIs(t, err, state.ErrVersion)
	assert.Equal(t, state.Fresh(), loaded)

	require.NoError(t, file.Save(loaded))

	loaded, err = state.File{Path: path}.Load()
	require.NoError(t, err)
	assert.Equal(t, state.Fresh(), loaded)
}

// TestStoresAgree runs the same session through both stores and gets the
// same state back from each.
func TestStoresAgree(t *testing.T) {
	t.Parallel()

	file := state.File{Path: filepath.Join(t.TempDir(), "state.json")}
	memory := &state.Memory{}

	for _, s := range []store{file, memory} {
		runDaemonSession(t, s, restartTime)
	}

	fromFile, err := file.Load()
	require.NoError(t, err)

	fromMemory, err := memory.Load()
	require.NoError(t, err)

	assertSameState(t, fromFile, fromMemory)
}
