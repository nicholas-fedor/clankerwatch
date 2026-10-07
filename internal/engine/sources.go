// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"context"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
)

// CredentialsSource reads Claude Code's login.
type CredentialsSource interface {
	// Read returns the current login.
	//
	// Returns:
	//   - claudecode.OAuth: the login.
	//   - error: claudecode.ErrNoCredentials, claudecode.ErrLoggedOut, or a read error.
	Read() (claudecode.OAuth, error)
}

// GlobalSource reads Claude Code's global config.
type GlobalSource interface {
	// Read returns the signed-in account and Claude Code's cached usage.
	//
	// Returns:
	//   - claudecode.GlobalState: the current, or previous, state.
	//   - error: a read or parse error.
	Read() (claudecode.GlobalState, error)
}

// ActivitySource reports whether Claude Code is in use.
type ActivitySource interface {
	// Active reports recent Claude Code activity.
	//
	// Parameters:
	//   - now: the current time.
	//
	// Returns:
	//   - bool: true while Claude Code appears to be in use.
	Active(now time.Time) bool
}

// Fetcher retrieves a usage payload.
type Fetcher interface {
	// Fetch requests the usage payload.
	//
	// Parameters:
	//   - ctx: cancellation and deadline for the request.
	//   - bearer: the OAuth access token.
	//
	// Returns:
	//   - []byte: the payload.
	//   - error: a *usage.FetchError for a classified failure.
	Fetch(ctx context.Context, bearer string) ([]byte, error)
}
