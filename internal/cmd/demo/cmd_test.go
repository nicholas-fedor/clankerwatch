// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package demo

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/demo/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// Errors returned by the test doubles.
var (
	errResolve = errors.New("resolve failed")
	errBus     = errors.New("bus unavailable")
)

// TestNewCommandMetadata checks the command is named and takes no arguments.
func TestNewCommandMetadata(t *testing.T) {
	t.Parallel()

	command := NewCommand(t.Context(), mocks.NewMockDemoer(t), resolveTo(config.Config{}, nil))

	assert.Equal(t, "demo", command.Name())
	assert.NotEmpty(t, command.Short)
	assert.NotEmpty(t, command.Long)
	assert.NotEmpty(t, command.Example)
	require.Error(t, command.Args(command, []string{"extra"}))
}

// TestRunPassesTheResolvedSettings checks the demoer gets the settings and
// the command's context.
func TestRunPassesTheResolvedSettings(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Mode: config.ModeCacheOnly, Home: "/home/tester", StateDir: "/state"}
	demoer := mocks.NewMockDemoer(t)
	demoer.EXPECT().Demo(t.Context(), cfg).Return(nil).Once()

	command := NewCommand(t.Context(), demoer, resolveTo(cfg, nil))

	require.NoError(t, command.RunE(command, nil))
}

// TestRunReturnsTheResolveError checks the demo never starts on bad
// settings.
func TestRunReturnsTheResolveError(t *testing.T) {
	t.Parallel()

	demoer := mocks.NewMockDemoer(t)
	command := NewCommand(t.Context(), demoer, resolveTo(config.Config{}, errResolve))

	err := command.RunE(command, nil)
	require.ErrorIs(t, err, errResolve)
	demoer.AssertNotCalled(t, "Demo", mock.Anything, mock.Anything)
}

// TestRunWrapsTheDemoError checks the failure names the command.
func TestRunWrapsTheDemoError(t *testing.T) {
	t.Parallel()

	demoer := mocks.NewMockDemoer(t)
	demoer.EXPECT().Demo(mock.Anything, mock.Anything).Return(errBus).Once()

	command := NewCommand(t.Context(), demoer, resolveTo(config.Config{}, nil))

	err := command.RunE(command, nil)
	require.ErrorIs(t, err, errBus)
	assert.Contains(t, err.Error(), "demo: ")
}

// resolveTo returns a resolver with a fixed result.
func resolveTo(cfg config.Config, err error) func() (config.Config, error) {
	return func() (config.Config, error) { return cfg, err }
}
