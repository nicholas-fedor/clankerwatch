// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config_test exercises building and validating settings through
// public APIs.
package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// testDirs returns directories rooted in a temporary directory.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - config.Dirs: home and state directories that never touch the real home.
func testDirs(t *testing.T) config.Dirs {
	t.Helper()

	root := t.TempDir()

	return config.Dirs{Home: filepath.Join(root, "home"), StateHome: filepath.Join(root, "state")}
}

// TestDefaultsOverriddenLikeTheCommandLine applies overrides the way the
// options package does and validates the result.
func TestDefaultsOverriddenLikeTheCommandLine(t *testing.T) {
	t.Parallel()

	dirs := testDirs(t)
	cfg := config.Default(dirs)

	mode, err := config.ParseMode(" cache-only ")
	require.NoError(t, err)

	thresholds, err := config.ParseThresholds("95, 50, 75, 50")
	require.NoError(t, err)

	cfg.Mode = mode
	cfg.Thresholds = thresholds
	cfg.Interval = 3 * time.Minute
	cfg.IdleInterval = 30 * time.Minute

	require.NoError(t, cfg.Validate())
	assert.Equal(t, config.ModeCacheOnly, cfg.Mode)
	assert.Equal(t, []float64{50, 75, 95}, cfg.Thresholds)
	assert.Equal(t, filepath.Join(dirs.StateHome, "clankerwatch", "state.json"), cfg.StateFile())

	warn, crit := cfg.ColorThresholds()
	assert.InDelta(t, 50.0, warn, 1e-9)
	assert.InDelta(t, 95.0, crit, 1e-9)
}

// TestDisabledAlertsKeepDefaultColors disables alerts with an empty list.
//
// Colors still need thresholds when alerts are off, so they fall back to the
// defaults rather than painting every bar critical.
func TestDisabledAlertsKeepDefaultColors(t *testing.T) {
	t.Parallel()

	cfg := config.Default(testDirs(t))

	thresholds, err := config.ParseThresholds("")
	require.NoError(t, err)

	cfg.Thresholds = thresholds

	require.NoError(t, cfg.Validate())

	warn, crit := cfg.ColorThresholds()
	defaults := config.DefaultThresholds()
	assert.InDelta(t, defaults[0], warn, 1e-9)
	assert.InDelta(t, defaults[1], crit, 1e-9)
}

// TestInvalidOverridesAreReported surfaces each bad override as its sentinel.
func TestInvalidOverridesAreReported(t *testing.T) {
	t.Parallel()

	_, err := config.ParseMode("turbo")
	require.ErrorIs(t, err, config.ErrInvalidMode)

	_, err = config.ParseThresholds("80,120")
	require.ErrorIs(t, err, config.ErrInvalidThreshold)

	cfg := config.Default(testDirs(t))
	cfg.Interval = time.Minute
	require.ErrorIs(t, cfg.Validate(), config.ErrIntervalTooShort)

	cfg = config.Default(testDirs(t))
	cfg.IdleInterval = config.MinInterval
	require.ErrorIs(t, cfg.Validate(), config.ErrIdleTooShort)
}

// TestMissingDirectoriesFailValidation refuses to start without a home or a
// state directory.
func TestMissingDirectoriesFailValidation(t *testing.T) {
	t.Parallel()

	cfg := config.Default(config.Dirs{})
	require.ErrorIs(t, cfg.Validate(), config.ErrNoHome)

	cfg.ClaudeDir = filepath.Join(t.TempDir(), "claude")
	require.ErrorIs(t, cfg.Validate(), config.ErrNoStateDir, "StateHome was empty")

	cfg.StateDir = t.TempDir()
	require.NoError(t, cfg.Validate())
}

// TestEnvironmentNamesArePrefixed keeps the daemon's variables in its own
// namespace, apart from Claude Code's own override.
func TestEnvironmentNamesArePrefixed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		config.EnvMode, config.EnvInterval, config.EnvIdleInterval, config.EnvNotify,
		config.EnvNotifyAuth, config.EnvLogLevel, config.EnvStateDir, config.EnvConfig,
	} {
		assert.Regexp(t, `^CLANKERWATCH_[A-Z_]+$`, name)
	}

	assert.Equal(t, "CLAUDE_CONFIG_DIR", config.EnvClaudeConfigDir)
}

// TestSettingsFileOverridesDefaults loads the default settings file and
// validates the result, the way the daemon starts.
func TestSettingsFileOverridesDefaults(t *testing.T) {
	t.Parallel()

	dirs := testDirs(t)
	dirs.ConfigHome = filepath.Join(t.TempDir(), "config")
	path := dirs.AppSettingsFile()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("# Slower polling, alerts only when nearly out.\n"+
		"interval: 10m\nidleInterval: 1h\nnotify: [99]\n"), 0o600))

	file, err := config.LoadOptionalFile(path)
	require.NoError(t, err)

	cfg := config.Default(dirs)
	require.NoError(t, file.Apply(&cfg))
	require.NoError(t, cfg.Validate())

	assert.Equal(t, 10*time.Minute, cfg.Interval)
	assert.Equal(t, time.Hour, cfg.IdleInterval)
	assert.Equal(t, []float64{99}, cfg.Thresholds)
	assert.Equal(t, config.ModeHybrid, cfg.Mode, "keys the file leaves out keep their defaults")
}

// TestSettingsFileValuesStillValidate catches a file whose values decode but
// cannot work together.
func TestSettingsFileValuesStillValidate(t *testing.T) {
	t.Parallel()

	file, err := config.DecodeFile([]byte("interval: 30s\n"))
	require.NoError(t, err)

	cfg := config.Default(testDirs(t))
	require.NoError(t, file.Apply(&cfg))
	require.ErrorIs(t, cfg.Validate(), config.ErrIntervalTooShort)
}

// TestSettingsDialogSaveFlow saves settings the way the daemon does for the
// settings dialog and starts from the saved file.
//
// The first save creates the file and its directory. A second save changes
// one key and keeps the rest, and both results load, apply, and validate.
func TestSettingsDialogSaveFlow(t *testing.T) {
	t.Parallel()

	dirs := testDirs(t)
	dirs.ConfigHome = filepath.Join(t.TempDir(), "config")
	path := dirs.AppSettingsFile()

	file, err := config.LoadOptionalFile(path)
	require.NoError(t, err)
	assert.Equal(t, config.File{}, file, "a missing file holds no settings")

	data, err := config.UpdateFile(nil, config.File{
		Mode:            new(string(config.ModeCacheOnly)),
		Interval:        new(3 * time.Minute),
		IdleInterval:    new(time.Hour),
		Notify:          new([]float64{95, 50}),
		NotifyAuth:      new(false),
		LogLevel:        nil,
		ClaudeConfigDir: nil,
		StateDir:        nil,
	})
	require.NoError(t, err)
	require.NoError(t, config.SaveFile(path, data))

	cfg := loadAndValidate(t, dirs, path)
	assert.Equal(t, config.ModeCacheOnly, cfg.Mode)
	assert.Equal(t, 3*time.Minute, cfg.Interval)
	assert.Equal(t, time.Hour, cfg.IdleInterval)
	assert.Equal(t, []float64{50, 95}, cfg.Thresholds)
	assert.False(t, cfg.NotifyAuth)

	saved, err := os.ReadFile(path)
	require.NoError(t, err)

	data, err = config.UpdateFile(saved, config.File{Interval: new(10 * time.Minute)})
	require.NoError(t, err)
	require.NoError(t, config.SaveFile(path, data))

	cfg = loadAndValidate(t, dirs, path)
	assert.Equal(t, 10*time.Minute, cfg.Interval)
	assert.Equal(t, config.ModeCacheOnly, cfg.Mode, "keys the second save leaves out stay")
	assert.Equal(t, []float64{50, 95}, cfg.Thresholds)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// loadAndValidate loads a settings file that must exist, applies it to the
// defaults, and validates the result.
//
// Parameters:
//   - t: test handle.
//   - dirs: the directories for the defaults.
//   - path: the settings file.
//
// Returns:
//   - config.Config: the validated settings.
func loadAndValidate(t *testing.T, dirs config.Dirs, path string) config.Config {
	t.Helper()

	file, err := config.LoadFile(path)
	require.NoError(t, err)

	cfg := config.Default(dirs)
	require.NoError(t, file.Apply(&cfg))
	require.NoError(t, cfg.Validate())

	return cfg
}
