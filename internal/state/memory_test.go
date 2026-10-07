// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMemoryLoadEmpty returns a fresh state before anything was saved.
func TestMemoryLoadEmpty(t *testing.T) {
	t.Parallel()

	var memory Memory

	got, err := memory.Load()
	require.NoError(t, err)
	assert.Equal(t, Fresh(), got)
}

// TestMemoryRoundTrip returns what was saved.
func TestMemoryRoundTrip(t *testing.T) {
	t.Parallel()

	var memory Memory

	require.NoError(t, memory.Save(sampleState()))

	got, err := memory.Load()
	require.NoError(t, err)
	assert.Equal(t, sampleState(), got)
}

// TestMemoryIsolatesCopies keeps the stored state independent of both the
// saved value and every loaded value.
func TestMemoryIsolatesCopies(t *testing.T) {
	t.Parallel()

	var memory Memory

	saved := sampleState()
	require.NoError(t, memory.Save(saved))

	saved.Data.Payload[2] = 'X'
	saved.Alerts.ReplaceIDs["session"] = 99

	loaded, err := memory.Load()
	require.NoError(t, err)
	assert.Equal(t, sampleState(), loaded)

	loaded.Data.Source = "claude-code"
	loaded.Alerts.Entries[0].Bar = "weekly"

	again, err := memory.Load()
	require.NoError(t, err)
	assert.Equal(t, sampleState(), again)
}

// TestMemorySaveError keeps the previous state when the new one cannot be
// copied.
func TestMemorySaveError(t *testing.T) {
	t.Parallel()

	var memory Memory

	require.NoError(t, memory.Save(sampleState()))

	broken := Fresh()
	broken.Data = &Data{Payload: json.RawMessage(`{`)}

	require.Error(t, memory.Save(broken))

	got, err := memory.Load()
	require.NoError(t, err)
	assert.Equal(t, sampleState(), got)
}

// TestMemoryConcurrentAccess is safe under the race detector.
func TestMemoryConcurrentAccess(t *testing.T) {
	t.Parallel()

	const (
		workers = 8
		rounds  = 50
	)

	var (
		memory Memory
		group  sync.WaitGroup
	)

	errs := make(chan error, workers*rounds*2)

	for range workers {
		group.Go(func() {
			for range rounds {
				errs <- memory.Save(sampleState())

				_, err := memory.Load()
				errs <- err
			}
		})
	}

	group.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	got, err := memory.Load()
	require.NoError(t, err)
	assert.Equal(t, sampleState(), got)
}
