// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package serve

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/serve/mocks"
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

	command := NewCommand(t.Context(), mocks.NewMockServer(t), resolveTo(config.Config{}, nil))

	assert.Equal(t, "serve", command.Name())
	assert.NotEmpty(t, command.Short)
	assert.NotEmpty(t, command.Long)
	assert.NotEmpty(t, command.Example)
	require.Error(t, command.Args(command, []string{"extra"}))
}

// TestRunPassesTheResolvedSettings checks the server gets the settings, the
// command's context, and a reload function.
func TestRunPassesTheResolvedSettings(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Mode: config.ModeCacheOnly, Home: "/home/tester", StateDir: "/state"}
	server := mocks.NewMockServer(t)
	server.EXPECT().Serve(t.Context(), cfg, mock.Anything).Return(nil).Once()

	command := NewCommand(t.Context(), server, resolveTo(cfg, nil))

	require.NoError(t, command.RunE(command, nil))
}

// TestRunPassesAReloadThatResolvesAgain checks the reload function runs the
// command's resolution each time it is called.
//
// The daemon calls it after a settings change, so it must see the settings
// file as it is then rather than the values from startup.
func TestRunPassesAReloadThatResolvesAgain(t *testing.T) {
	t.Parallel()

	first := config.Config{Mode: config.ModeHybrid, Home: "/home/tester", StateDir: "/state"}
	second := config.Config{Mode: config.ModeCacheOnly, Home: "/home/tester", StateDir: "/state"}
	results := []config.Config{first, second}
	calls := 0

	resolve := func() (config.Config, error) {
		cfg := results[min(calls, len(results)-1)]
		calls++

		return cfg, nil
	}

	server := mocks.NewMockServer(t)
	server.EXPECT().Serve(t.Context(), first, mock.Anything).
		RunAndReturn(func(_ context.Context, _ config.Config, reload func() (config.Config, error)) error {
			require.NotNil(t, reload)

			cfg, err := reload()
			require.NoError(t, err)
			assert.Equal(t, second, cfg)

			return nil
		}).Once()

	command := NewCommand(t.Context(), server, resolve)

	require.NoError(t, command.RunE(command, nil))
	assert.Equal(t, 2, calls, "one resolution at startup and one per reload")
}

// TestRunPassesAReloadThatReportsErrors checks a failed resolution reaches
// the daemon through reload.
func TestRunPassesAReloadThatReportsErrors(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Mode: config.ModeHybrid, Home: "/home/tester", StateDir: "/state"}
	calls := 0

	resolve := func() (config.Config, error) {
		calls++
		if calls > 1 {
			return config.Config{}, errResolve
		}

		return cfg, nil
	}

	server := mocks.NewMockServer(t)
	server.EXPECT().Serve(t.Context(), cfg, mock.Anything).
		RunAndReturn(func(_ context.Context, _ config.Config, reload func() (config.Config, error)) error {
			_, err := reload()
			require.ErrorIs(t, err, errResolve)

			return nil
		}).Once()

	command := NewCommand(t.Context(), server, resolve)

	require.NoError(t, command.RunE(command, nil))
}

// TestRunReturnsTheResolveError checks the server never starts on bad
// settings.
func TestRunReturnsTheResolveError(t *testing.T) {
	t.Parallel()

	server := mocks.NewMockServer(t)
	command := NewCommand(t.Context(), server, resolveTo(config.Config{}, errResolve))

	err := command.RunE(command, nil)
	require.ErrorIs(t, err, errResolve)
	server.AssertNotCalled(t, "Serve", mock.Anything, mock.Anything, mock.Anything)
}

// TestRunWrapsTheServeError checks the failure names the command.
func TestRunWrapsTheServeError(t *testing.T) {
	t.Parallel()

	server := mocks.NewMockServer(t)
	server.EXPECT().Serve(mock.Anything, mock.Anything, mock.Anything).Return(errBus).Once()

	command := NewCommand(t.Context(), server, resolveTo(config.Config{}, nil))

	err := command.RunE(command, nil)
	require.ErrorIs(t, err, errBus)
	assert.Contains(t, err.Error(), "serve: ")
}

// resolveTo returns a resolver with a fixed result.
func resolveTo(cfg config.Config, err error) func() (config.Config, error) {
	return func() (config.Config, error) { return cfg, err }
}
