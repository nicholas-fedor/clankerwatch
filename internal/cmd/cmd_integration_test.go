// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd"
	demoMocks "github.com/nicholas-fedor/clankerwatch/internal/cmd/demo/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/mocks"
	notifyMocks "github.com/nicholas-fedor/clankerwatch/internal/cmd/notifytest/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	serveMocks "github.com/nicholas-fedor/clankerwatch/internal/cmd/serve/mocks"
	statusMocks "github.com/nicholas-fedor/clankerwatch/internal/cmd/status/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/daemon"
)

// errBus is returned by the failing service doubles.
var errBus = errors.New("bus unavailable")

// testDirs are XDG directories that exist only as strings.
var testDirs = config.Dirs{Home: "/home/tester", StateHome: "/home/tester/.local/state"}

// doubles holds the mocks behind one command tree.
type doubles struct {
	serve  *serveMocks.MockServer
	demo   *demoMocks.MockDemoer
	status *statusMocks.MockReporter
	notify *notifyMocks.MockSender
	levels *mocks.MockLevelSetter
	env    map[string]string
}

// result is the outcome of one execution.
type result struct {
	err    error
	stdout string
	stderr string
}

// TestNewRootHelp checks the bare invocation and --help print the help.
func TestNewRootHelp(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"--help"}} {
		got := execute(t, newDoubles(t), args...)

		require.NoError(t, got.err)
		assert.Contains(t, got.stdout, "Usage:")

		for _, name := range []string{"serve", "status", "demo", "notify-test", "version"} {
			assert.Contains(t, got.stdout, name)
		}
	}
}

// TestNewRootVersion checks the version line reaches stdout unchanged.
func TestNewRootVersion(t *testing.T) {
	t.Parallel()

	got := execute(t, newDoubles(t), "version")

	require.NoError(t, got.err)
	assert.Equal(t, "clankerwatch v9.9.9 (abc1234, now)\n", got.stdout)
	assert.Empty(t, got.stderr)
}

// TestNewRootStatusJSON checks status resolves settings and prints JSON.
//
// The environment selects cache-only and a flag overrides the interval, so
// the reporter must see both sources merged.
func TestNewRootStatusJSON(t *testing.T) {
	t.Parallel()

	mocked := newDoubles(t)
	mocked.env[config.EnvMode] = string(config.ModeCacheOnly)
	mocked.levels.EXPECT().SetLevel(mock.Anything).Return().Once()
	mocked.status.EXPECT().Status(mock.Anything, mock.MatchedBy(func(cfg config.Config) bool {
		return cfg.Mode == config.ModeCacheOnly && cfg.Interval == 3*time.Minute
	})).Return(daemon.Report{
		Version:  "v9.9.9",
		Mode:     config.ModeCacheOnly,
		Snapshot: json.RawMessage(`{"status":"no_data"}`),
	}).Once()

	got := execute(t, mocked, "status", "--json", "--interval", "3m")
	require.NoError(t, got.err)

	var decoded struct {
		Snapshot struct {
			Status string `json:"status"`
		} `json:"snapshot"`
		Mode string `json:"mode"`
	}

	require.NoError(t, json.Unmarshal([]byte(got.stdout), &decoded))
	assert.Equal(t, "cache-only", decoded.Mode)
	assert.Equal(t, "no_data", decoded.Snapshot.Status)
}

// TestNewRootInvalidSettings checks bad values stop every command that
// resolves settings before any service runs.
func TestNewRootInvalidSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  map[string]string
		name string
		args []string
	}{
		{name: "status flag", args: []string{"status", "--mode", "bogus"}},
		{name: "serve env", args: []string{"serve"}, env: map[string]string{config.EnvInterval: "1s"}},
		{name: "demo env", args: []string{"demo"}, env: map[string]string{config.EnvNotify: "150"}},
		{name: "persistent flag first", args: []string{"--log-level", "loud", "status"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mocked := newDoubles(t)
			maps.Copy(mocked.env, tt.env)

			got := execute(t, mocked, tt.args...)

			require.ErrorIs(t, got.err, options.ErrInvalidSetting)
			assert.Empty(t, got.stdout)
		})
	}
}

// TestNewRootServices checks each service command calls its dependency and
// wraps its failure.
func TestNewRootServices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect func(mocked *doubles, err error)
		name   string
		prefix string
	}{
		{
			name: "serve", prefix: "serve: ",
			expect: func(mocked *doubles, err error) {
				mocked.levels.EXPECT().SetLevel(mock.Anything).Return().Once()
				mocked.serve.EXPECT().Serve(mock.Anything, mock.Anything, mock.Anything).Return(err).Once()
			},
		},
		{
			name: "demo", prefix: "demo: ",
			expect: func(mocked *doubles, err error) {
				mocked.levels.EXPECT().SetLevel(mock.Anything).Return().Once()
				mocked.demo.EXPECT().Demo(mock.Anything, mock.Anything).Return(err).Once()
			},
		},
		{
			name: "notify-test", prefix: "notify-test: ",
			expect: func(mocked *doubles, err error) {
				mocked.notify.EXPECT().NotifyTest(mock.Anything).Return(err).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" succeeds", func(t *testing.T) {
			t.Parallel()

			mocked := newDoubles(t)
			tt.expect(mocked, nil)

			require.NoError(t, execute(t, mocked, tt.name).err)
		})

		t.Run(tt.name+" fails", func(t *testing.T) {
			t.Parallel()

			mocked := newDoubles(t)
			tt.expect(mocked, errBus)

			got := execute(t, mocked, tt.name)
			require.ErrorIs(t, got.err, errBus)
			assert.Contains(t, got.err.Error(), tt.prefix)
		})
	}
}

// TestNewRootServeReloadResolvesAgain checks serve hands the daemon a reload
// that resolves the settings the same way startup did.
//
// The first reload sees the unchanged environment and flags, so it must match
// the startup settings. The second runs after the environment changes, so it
// must pick up the change, which proves each call resolves afresh. The third
// sees an invalid environment and must report it.
func TestNewRootServeReloadResolvesAgain(t *testing.T) {
	t.Parallel()

	mocked := newDoubles(t)
	mocked.env[config.EnvMode] = string(config.ModeCacheOnly)
	mocked.levels.EXPECT().SetLevel(mock.Anything).Return().Times(3)
	mocked.serve.EXPECT().Serve(mock.Anything, mock.MatchedBy(func(cfg config.Config) bool {
		return cfg.Mode == config.ModeCacheOnly && cfg.Interval == 3*time.Minute
	}), mock.Anything).RunAndReturn(func(_ context.Context, cfg config.Config, reload func() (config.Config, error)) error {
		require.NotNil(t, reload)

		again, err := reload()
		require.NoError(t, err)
		assert.Equal(t, cfg, again)

		mocked.env[config.EnvMode] = string(config.ModeHybrid)

		changed, err := reload()
		require.NoError(t, err)
		assert.Equal(t, config.ModeHybrid, changed.Mode)
		assert.Equal(t, 3*time.Minute, changed.Interval, "the flag still applies")

		mocked.env[config.EnvMode] = "bogus"

		_, err = reload()
		require.ErrorIs(t, err, options.ErrInvalidSetting)

		return nil
	}).Once()

	require.NoError(t, execute(t, mocked, "serve", "--interval", "3m").err)
}

// TestNewRootRejectsMistakes checks typos and stray arguments fail.
func TestNewRootRejectsMistakes(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"srve"}, {"status", "extra"}, {"version", "--bogus"}} {
		got := execute(t, newDoubles(t), args...)

		require.Error(t, got.err, "%v", args)
		assert.Empty(t, got.stdout, "%v", args)
		assert.Empty(t, got.stderr, "errors are left to the caller for %v", args)
	}
}

// newDoubles returns fresh mocks and an empty environment.
func newDoubles(t *testing.T) *doubles {
	t.Helper()

	return &doubles{
		serve:  serveMocks.NewMockServer(t),
		demo:   demoMocks.NewMockDemoer(t),
		status: statusMocks.NewMockReporter(t),
		notify: notifyMocks.NewMockSender(t),
		levels: mocks.NewMockLevelSetter(t),
		env:    map[string]string{},
	}
}

// execute runs the command tree with args against mocked.
func execute(t *testing.T, mocked *doubles, args ...string) result {
	t.Helper()

	var stdout, stderr bytes.Buffer

	root := cmd.NewRoot(t.Context(), cmd.Dependencies{
		Serve:      mocked.serve,
		Demo:       mocked.demo,
		Status:     mocked.status,
		NotifyTest: mocked.notify,
		Levels:     mocked.levels,
		Stdout:     &stdout,
		LookupEnv: func(key string) (string, bool) {
			value, ok := mocked.env[key]

			return value, ok
		},
		Dirs:    testDirs,
		Version: "v9.9.9 (abc1234, now)",
	})
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()

	return result{err: err, stdout: stdout.String(), stderr: stderr.String()}
}
