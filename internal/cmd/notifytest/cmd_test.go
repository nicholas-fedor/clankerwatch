// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notifytest

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/notifytest/mocks"
)

// errNoServer is returned when no notification server answers.
var errNoServer = errors.New("no notification server")

// TestNewCommandMetadata checks the command is named and takes no arguments.
func TestNewCommandMetadata(t *testing.T) {
	t.Parallel()

	command := NewCommand(t.Context(), mocks.NewMockSender(t))

	assert.Equal(t, "notify-test", command.Name())
	assert.NotEmpty(t, command.Short)
	assert.NotEmpty(t, command.Long)
	assert.NotEmpty(t, command.Example)
	require.Error(t, command.Args(command, []string{"extra"}))
}

// TestRunSendsOneNotification checks the sender is called once with the
// command's context.
func TestRunSendsOneNotification(t *testing.T) {
	t.Parallel()

	sender := mocks.NewMockSender(t)
	sender.EXPECT().NotifyTest(t.Context()).Return(nil).Once()

	command := NewCommand(t.Context(), sender)

	require.NoError(t, command.RunE(command, nil))
}

// TestRunWrapsTheSendError checks the failure names the command.
func TestRunWrapsTheSendError(t *testing.T) {
	t.Parallel()

	sender := mocks.NewMockSender(t)
	sender.EXPECT().NotifyTest(t.Context()).Return(errNoServer).Once()

	command := NewCommand(t.Context(), sender)

	err := command.RunE(command, nil)
	require.ErrorIs(t, err, errNoServer)
	assert.Contains(t, err.Error(), "notify-test: ")
}
