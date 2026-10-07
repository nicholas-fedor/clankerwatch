// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package options

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errDiskFull is the failure failWriter returns.
var errDiskFull = errors.New("disk full")

// failWriter fails every write so the error paths are reachable.
type failWriter struct{}

// Write always fails.
func (failWriter) Write([]byte) (int, error) {
	return 0, errDiskFull
}

// TestWriteString checks the exact bytes reach the writer.
func TestWriteString(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := WriteString(&out, "clankerwatch v1\n")
	require.NoError(t, err)
	assert.Equal(t, "clankerwatch v1\n", out.String())
}

// TestWriteStringWrapsTheFailure checks both sentinels survive.
//
// Callers match ErrWriteOutput, and the cause has to stay visible for the
// diagnostic line.
func TestWriteStringWrapsTheFailure(t *testing.T) {
	t.Parallel()

	err := WriteString(failWriter{}, "lost")
	require.ErrorIs(t, err, ErrWriteOutput)
	assert.ErrorIs(t, err, errDiskFull)
}
