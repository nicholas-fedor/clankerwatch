// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import "context"

// nopLogger discards every event.
type nopLogger struct{}

// Log keys.
const (
	logKeyErr     = "err"
	logKeyRetryAt = "retry_at"
	logKeyAge     = "age"
	logKeyBars    = "bars"
)

// Debug discards a debug event.
func (nopLogger) Debug(context.Context, string, ...any) {}

// Info discards an informational event.
func (nopLogger) Info(context.Context, string, ...any) {}

// Warn discards a recoverable event.
func (nopLogger) Warn(context.Context, string, ...any) {}
