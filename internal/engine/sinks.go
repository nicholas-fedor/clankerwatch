// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"context"

	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
)

// Notifier sends desktop notifications.
type Notifier interface {
	// Send posts a notification.
	//
	// Parameters:
	//   - ctx: cancellation for the call.
	//   - message: the notification.
	//
	// Returns:
	//   - uint32: the notification ID.
	//   - error: the send error.
	Send(ctx context.Context, message notify.Message) (uint32, error)
}

// Store loads and saves the engine state.
type Store interface {
	// Load returns the saved state.
	//
	// Returns:
	//   - state.State: the saved state, or a fresh one.
	//   - error: a read or decode error. A fresh state is still returned.
	Load() (state.State, error)

	// Save persists the state.
	//
	// Parameters:
	//   - current: the state to save.
	//
	// Returns:
	//   - error: a write error.
	Save(current state.State) error
}

// Logger records diagnostic events.
//
// Messages are static. args are key-value pairs, as with log/slog. A nil
// Deps.Logger is a no-op.
type Logger interface {
	// Debug records a debug event.
	//
	// Parameters:
	//   - ctx: cancellation and request scope.
	//   - msg: static message.
	//   - args: key-value pairs.
	Debug(ctx context.Context, msg string, args ...any)

	// Info records an informational event.
	//
	// Parameters:
	//   - ctx: cancellation and request scope.
	//   - msg: static message.
	//   - args: key-value pairs.
	Info(ctx context.Context, msg string, args ...any)

	// Warn records a recoverable event.
	//
	// Parameters:
	//   - ctx: cancellation and request scope.
	//   - msg: static message.
	//   - args: key-value pairs.
	Warn(ctx context.Context, msg string, args ...any)
}
