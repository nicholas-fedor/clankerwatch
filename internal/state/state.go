// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
)

// State is what survives a daemon restart. It never contains the token.
type State struct {
	// LastAttempt is when the daemon last called the endpoint.
	LastAttempt time.Time `json:"lastAttempt,omitzero"`

	// Data is the last good usage payload, or nil.
	Data *Data `json:"data,omitempty"`

	// AuthBlock is the rejected token, or nil.
	AuthBlock *schedule.AuthBlock `json:"authBlock,omitempty"`

	// Backoff is the failure state.
	Backoff schedule.Backoff `json:"backoff,omitzero"`

	// Alerts remembers delivered notifications.
	Alerts notify.Ledger `json:"alerts,omitzero"`

	// Version is the state format version.
	Version int `json:"version"`
}

// Data is the last good usage payload.
//
// The raw payload is kept, so a fixed parser applies to old data after an
// upgrade.
type Data struct {
	// FetchedAt is when the payload was fetched.
	FetchedAt time.Time `json:"fetchedAt"`

	// Source is api or claude-code.
	Source string `json:"source"`

	// AccountUUID is the account the payload belongs to.
	AccountUUID string `json:"accountUuid,omitempty"`

	// Payload is the raw usage payload.
	Payload json.RawMessage `json:"payload"`
}

// Version is the current state format.
const Version = 1

// Fresh returns an empty state in the current format.
//
// Returns:
//   - State: the empty state.
func Fresh() State {
	return State{
		Version:     Version,
		Data:        nil,
		LastAttempt: time.Time{},
		Backoff:     schedule.Backoff{},
		AuthBlock:   nil,
		Alerts:      notify.Ledger{},
	}
}

// clone deep-copies a state through JSON.
//
// Parameters:
//   - original: the state to copy.
//
// Returns:
//   - State: the copy.
//   - error: an encode or decode error.
func clone(original State) (State, error) {
	content, err := json.Marshal(original)
	if err != nil {
		return State{}, fmt.Errorf("copy state: %w", err)
	}

	var copied State

	err = json.Unmarshal(content, &copied)
	if err != nil {
		return State{}, fmt.Errorf("copy state: %w", err)
	}

	return copied, nil
}
