// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNopLoggerDiscardsEverything checks the default logger accepts any event.
//
// The engine logs with arbitrary key-value pairs, including odd counts, so
// the default must accept anything without panicking.
func TestNopLoggerDiscardsEverything(t *testing.T) {
	t.Parallel()

	var logger Logger = nopLogger{}

	assert.NotPanics(t, func() {
		logger.Debug(t.Context(), "debug", logKeyErr, errBoom)
		logger.Info(t.Context(), "info")
		logger.Warn(t.Context(), "warn", logKeyRetryAt)
	})
}
