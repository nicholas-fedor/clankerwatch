// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package version

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
)

// failWriter fails every write so the error path is reachable.
type failWriter struct{}

// Write always fails.
func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

// TestNewCommandMetadata checks the command is named and takes no arguments.
func TestNewCommandMetadata(t *testing.T) {
	t.Parallel()

	command := NewCommand(&bytes.Buffer{}, "v1")

	assert.Equal(t, "version", command.Name())
	assert.NotEmpty(t, command.Short)
	assert.NotEmpty(t, command.Long)
	require.Error(t, command.Args(command, []string{"extra"}))
}

// TestRunPrintsTheVersionLine checks the program name prefixes the line.
func TestRunPrintsTheVersionLine(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	command := NewCommand(&stdout, "v0.1.0 (abc1234, 2026-10-06T12:00:00Z)")

	require.NoError(t, command.RunE(command, nil))
	assert.Equal(t, "clankerwatch v0.1.0 (abc1234, 2026-10-06T12:00:00Z)\n", stdout.String())
}

// TestRunReportsAWriteFailure checks a closed stdout is an error.
func TestRunReportsAWriteFailure(t *testing.T) {
	t.Parallel()

	command := NewCommand(failWriter{}, "v1")

	assert.ErrorIs(t, command.RunE(command, nil), options.ErrWriteOutput)
}
