// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/version"
)

// errBoom is a runtime failure that is not a setting.
var errBoom = errors.New("boom")

// outcome is what one execution produced.
type outcome struct {
	stdout string
	stderr string
	code   int
}

// TestCodeFor checks the exit code per error kind.
//
// systemd's RestartPreventExitStatus relies on 2 meaning a configuration
// mistake that a restart cannot fix.
func TestCodeFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, exitInvalidSetting, codeFor(fmt.Errorf("resolve: %w", options.ErrInvalidSetting)))
	assert.Equal(t, exitInvalidSetting, codeFor(errors.Join(errBoom, options.ErrInvalidSetting)))
	assert.Equal(t, exitFailure, codeFor(errBoom))
}

// TestVersionLine checks the rendered build metadata.
func TestVersionLine(t *testing.T) {
	t.Parallel()

	line := versionLine(version.Info{Version: "v0.1.0", CommitSHA: "abc1234", BuildTime: "2026-10-06T12:00:00Z"})

	assert.Equal(t, "v0.1.0 (abc1234, 2026-10-06T12:00:00Z)", line)
}

// TestExecuteVersion checks the version command exits 0 with one line.
func TestExecuteVersion(t *testing.T) {
	t.Parallel()

	got := executeArgs(t, envMap(nil), "version")

	assert.Equal(t, exitOK, got.code)
	assert.Equal(t, "clankerwatch "+versionLine(version.Current())+"\n", got.stdout)
	assert.Empty(t, got.stderr)
}

// TestExecuteHelp checks the bare invocation prints help and exits 0.
func TestExecuteHelp(t *testing.T) {
	t.Parallel()

	got := executeArgs(t, envMap(nil))

	assert.Equal(t, exitOK, got.code)
	assert.Contains(t, got.stdout, "Usage:")
}

// TestExecuteInvalidSetting checks a bad value exits 2 with one error line.
func TestExecuteInvalidSetting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  map[string]string
		name string
		args []string
	}{
		{name: "flag", args: []string{"status", "--mode", "bogus"}},
		{name: "environment", args: []string{"status"}, env: map[string]string{config.EnvInterval: "1s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := executeArgs(t, envMap(tt.env), tt.args...)

			assert.Equal(t, exitInvalidSetting, got.code)
			assert.Empty(t, got.stdout)
			assert.Regexp(t, `^clankerwatch: resolve settings: invalid setting: .*\n$`, got.stderr)
		})
	}
}

// TestExecuteUnknownCommand checks a typo exits 1 with an error line.
func TestExecuteUnknownCommand(t *testing.T) {
	t.Parallel()

	got := executeArgs(t, envMap(nil), "srve")

	assert.Equal(t, exitFailure, got.code)
	assert.Empty(t, got.stdout)
	assert.Contains(t, got.stderr, `clankerwatch: unknown command "srve"`)
}

// TestExecuteStatusWithInjectedEnvironment checks status runs offline from
// the injected reader.
//
// CLAUDE_CONFIG_DIR and the state directory point at temporary directories,
// so neither ~/.claude nor the real state is read.
func TestExecuteStatusWithInjectedEnvironment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env := envMap(map[string]string{
		config.EnvClaudeConfigDir: filepath.Join(dir, "claude"),
		config.EnvStateDir:        filepath.Join(dir, "state"),
		config.EnvMode:            string(config.ModeCacheOnly),
	})

	got := executeArgs(t, env, "status", "--json")
	require.Equal(t, exitOK, got.code, got.stderr)

	report := decodeReport(t, got.stdout)
	assert.Equal(t, "cache-only", report.Mode)
	assert.Equal(t, filepath.Join(dir, "claude", ".credentials.json"), report.Credentials)
	assert.Equal(t, filepath.Join(dir, "state", "state.json"), report.StateFile)
	assert.Equal(t, "no_data", report.Snapshot.Status)
}

// TestExecuteStatusUsesXDGDirs checks the default paths follow the XDG
// environment.
//
// config.SystemDirs reads adrg/xdg, which caches the environment, so the test
// reloads it after pointing every directory at a temporary home and again
// once the environment is restored.
func TestExecuteStatusUsesXDGDirs(t *testing.T) {
	t.Cleanup(xdg.Reload)

	home := t.TempDir()
	stateHome := filepath.Join(home, "state")
	configHome := filepath.Join(home, "config")
	claudeDir := filepath.Join(home, "claude")
	settings := filepath.Join(configHome, "clankerwatch", "config.yaml")

	require.NoError(t, os.MkdirAll(filepath.Dir(settings), 0o700))
	require.NoError(t, os.WriteFile(settings, []byte("interval: 7m\n"), 0o600))

	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv(config.EnvClaudeConfigDir, claudeDir)

	for _, key := range []string{
		config.EnvMode, config.EnvInterval, config.EnvIdleInterval, config.EnvNotify,
		config.EnvNotifyAuth, config.EnvLogLevel, config.EnvStateDir, config.EnvConfig,
	} {
		unsetenv(t, key)
	}

	xdg.Reload()

	got := executeArgs(t, os.LookupEnv, "status", "--json")
	require.Equal(t, exitOK, got.code, got.stderr)

	report := decodeReport(t, got.stdout)
	assert.Equal(t, "hybrid", report.Mode)
	assert.Equal(t, filepath.Join(claudeDir, ".credentials.json"), report.Credentials)
	assert.Equal(t, filepath.Join(stateHome, "clankerwatch", "state.json"), report.StateFile)
	assert.Equal(t, settings, report.SettingsFile)
	assert.Equal(t, int64(7*time.Minute), report.Interval, "the settings file in XDG_CONFIG_HOME applies")
	assert.Equal(t, "logged_out", report.Snapshot.Status)
}

// TestRun checks the process entry point reads os.Args.
func TestRun(t *testing.T) {
	arguments := os.Args

	t.Cleanup(func() { os.Args = arguments })

	os.Args = []string{options.Name, "status", "--mode", "bogus"}

	assert.Equal(t, exitInvalidSetting, Run(t.Context()))
}

// executeArgs runs the command tree with args and lookupEnv.
func executeArgs(t *testing.T, lookupEnv options.LookupEnv, args ...string) outcome {
	t.Helper()

	var stdout, stderr bytes.Buffer

	code := execute(t.Context(), &stdout, &stderr, args, lookupEnv)

	return outcome{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// envMap returns an environment reader backed by values.
func envMap(values map[string]string) options.LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]

		return value, ok
	}
}

// unsetenv removes key for the duration of the test.
func unsetenv(t *testing.T, key string) {
	t.Helper()

	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

// statusReport is the part of the status JSON the tests read.
type statusReport struct {
	Snapshot struct {
		Status string `json:"status"`
	} `json:"snapshot"`
	Mode         string `json:"mode"`
	Credentials  string `json:"credentials"`
	StateFile    string `json:"stateFile"`
	SettingsFile string `json:"settingsFile"`
	Interval     int64  `json:"interval"`
}

// decodeReport parses the status JSON.
func decodeReport(t *testing.T, stdout string) statusReport {
	t.Helper()

	var report statusReport

	require.NoError(t, json.Unmarshal([]byte(stdout), &report), stdout)

	return report
}
