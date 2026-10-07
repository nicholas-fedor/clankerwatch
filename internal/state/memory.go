// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import "sync"

// Memory keeps the state in memory, for demo mode.
type Memory struct {
	saved State
	mu    sync.Mutex
}

// Load returns a copy of the saved state.
//
// Returns:
//   - State: the saved state, or a fresh one.
//   - error: a copy error.
func (m *Memory) Load() (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.saved.Version == 0 {
		return Fresh(), nil
	}

	return clone(m.saved)
}

// Save stores a deep copy of current.
//
// Parameters:
//   - current: the state to save.
//
// Returns:
//   - error: a copy error.
func (m *Memory) Save(current State) error {
	copied, err := clone(current)
	if err != nil {
		return err
	}

	m.mu.Lock()

	m.saved = copied
	m.mu.Unlock()

	return nil
}
