// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/mocks"
	serveMocks "github.com/nicholas-fedor/clankerwatch/internal/cmd/serve/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// testDirs are XDG directories that exist only as strings.
var testDirs = config.Dirs{Home: "/home/tester", StateHome: "/home/tester/.local/state"}

// TestLookupEnvDefaultsToOS checks a missing reader falls back to the process
// environment.
func TestLookupEnvDefaultsToOS(t *testing.T) {
	t.Parallel()

	got, gotOK := lookupEnv(Dependencies{})("PATH")
	want, wantOK := os.LookupEnv("PATH")

	assert.Equal(t, want, got)
	assert.Equal(t, wantOK, gotOK)
}

// TestLookupEnvUsesTheInjectedReader checks tests can replace the
// environment.
func TestLookupEnvUsesTheInjectedReader(t *testing.T) {
	t.Parallel()

	deps := Dependencies{LookupEnv: func(key string) (string, bool) { return "injected " + key, true }}

	value, ok := lookupEnv(deps)("KEY")

	assert.True(t, ok)
	assert.Equal(t, "injected KEY", value)
}

// TestDocRoot checks the documentation tree carries every command and flag.
//
// tools/docgen walks this tree without dependencies, so building it must not
// need any.
func TestDocRoot(t *testing.T) {
	t.Parallel()

	root := DocRoot(t.Context())

	assert.True(t, root.SilenceUsage)
	assert.True(t, root.SilenceErrors)

	names := make([]string, 0, len(root.Commands()))
	for _, command := range root.Commands() {
		names = append(names, command.Name())
	}

	assert.ElementsMatch(t, []string{"serve", "status", "demo", "notify-test", "version"}, names)

	for _, flag := range []string{
		"mode", "interval", "idle-interval", "notify", "notify-auth", "log-level", "claude-config-dir", "state-dir",
	} {
		assert.NotNil(t, root.PersistentFlags().Lookup(flag), flag)
	}
}

// TestRunHelpWritesHelp checks the bare root prints its help.
func TestRunHelpWritesHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	root := DocRoot(t.Context())
	root.SetOut(&stdout)

	require.NoError(t, runHelp(root, nil))
	assert.Contains(t, stdout.String(), "Usage:")
	assert.Contains(t, stdout.String(), "notify-test")
}

// TestNewRootReportsTheLogLevel checks the resolved level reaches the logger.
//
// The logger was wired before cobra parsed the flags, so this hook is the only
// way --log-level takes effect.
func TestNewRootReportsTheLogLevel(t *testing.T) {
	t.Parallel()

	levels := mocks.NewMockLevelSetter(t)
	levels.EXPECT().SetLevel(slog.LevelDebug).Return().Once()

	server := serveMocks.NewMockServer(t)
	server.EXPECT().Serve(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	root := NewRoot(t.Context(), Dependencies{
		Serve:     server,
		Levels:    levels,
		Stdout:    &bytes.Buffer{},
		LookupEnv: func(string) (string, bool) { return "", false },
		Dirs:      testDirs,
	})
	root.SetArgs([]string{"serve", "--log-level", "debug"})

	require.NoError(t, root.Execute())
}

// TestNewRootWithoutLevels checks a missing level hook is tolerated.
func TestNewRootWithoutLevels(t *testing.T) {
	t.Parallel()

	server := serveMocks.NewMockServer(t)
	server.EXPECT().Serve(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	root := NewRoot(t.Context(), Dependencies{
		Serve:     server,
		Stdout:    &bytes.Buffer{},
		LookupEnv: func(string) (string, bool) { return "", false },
		Dirs:      testDirs,
	})
	root.SetArgs([]string{"serve"})

	require.NoError(t, root.Execute())
}
