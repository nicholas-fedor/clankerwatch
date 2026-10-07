// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package options

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// testDirs are XDG directories that exist only as strings.
//
// Resolution never touches the file system, so no temporary directory is
// needed.
var testDirs = config.Dirs{Home: "/home/tester", StateHome: "/home/tester/.local/state"}

// TestNew checks the pre-parse values match the config defaults.
//
// The flag help prints these values, so a drift would document a default the
// daemon does not use.
func TestNew(t *testing.T) {
	t.Parallel()

	opts := New()
	defaults := config.Default(testDirs)

	require.NotNil(t, opts)
	assert.Equal(t, string(defaults.Mode), opts.Mode)
	assert.Equal(t, "80,95", opts.Notify)
	assert.Equal(t, "info", opts.LogLevel)
	assert.Empty(t, opts.ClaudeConfigDir)
	assert.Empty(t, opts.StateDir)
	assert.Equal(t, defaults.Interval, opts.Interval)
	assert.Equal(t, defaults.IdleInterval, opts.IdleInterval)
	assert.Equal(t, defaults.NotifyAuth, opts.NotifyAuth)

	thresholds, err := config.ParseThresholds(opts.Notify)
	require.NoError(t, err)
	assert.Equal(t, defaults.Thresholds, thresholds)
}

// TestBindPersistent checks every flag reaches its destination.
func TestBindPersistent(t *testing.T) {
	t.Parallel()

	opts := New()
	root := &cobra.Command{Use: Name}
	BindPersistent(root, opts)

	err := root.PersistentFlags().Parse([]string{
		"--config", "/settings.yaml",
		"--mode", "cache-only",
		"--interval", "3m",
		"--idle-interval", "30m",
		"--notify", "50",
		"--notify-auth=false",
		"--log-level", "debug",
		"--claude-config-dir", "/claude",
		"--state-dir", "/state",
	})
	require.NoError(t, err)

	assert.Equal(t, Options{
		Config:          "/settings.yaml",
		Mode:            "cache-only",
		Notify:          "50",
		LogLevel:        "debug",
		ClaudeConfigDir: "/claude",
		StateDir:        "/state",
		Interval:        3 * time.Minute,
		IdleInterval:    30 * time.Minute,
		NotifyAuth:      false,
	}, *opts)

	for _, item := range settings {
		flag := root.PersistentFlags().Lookup(item.flag)
		require.NotNil(t, flag, item.flag)
		assert.Contains(t, flag.Usage, "$"+item.env, "usage of --%s names its variable", item.flag)
	}
}

// TestResolveSettingsDefaults checks nothing set yields config.Default.
func TestResolveSettingsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveSettings(newFlags(t), envMap(nil), testDirs)
	require.NoError(t, err)
	assert.Equal(t, config.Default(testDirs), cfg)
	require.NotNil(t, cfg.Overrides)
	assert.Empty(t, cfg.Overrides)
}

// TestResolveSettingsOverrides records which flag or variable set each key.
//
// The settings dialog locks a key that a flag or variable sets, and names
// that source, because a value saved to the file would never take effect.
func TestResolveSettingsOverrides(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		config.KeyMode:            "cache-only",
		config.KeyInterval:        "3m",
		config.KeyIdleInterval:    "30m",
		config.KeyNotify:          "50",
		config.KeyNotifyAuth:      "false",
		config.KeyLogLevel:        "debug",
		config.KeyClaudeConfigDir: "/claude",
		config.KeyStateDir:        "/state",
	}

	require.Len(t, settings, len(values), "every setting has a test value")

	for _, item := range settings {
		t.Run(item.key, func(t *testing.T) {
			t.Parallel()

			value, ok := values[item.key]
			require.True(t, ok, "test value for %s", item.key)

			flagArg := "--" + item.flag + "=" + value
			env := envMap(map[string]string{item.env: value})

			cfg, err := ResolveSettings(newFlags(t, flagArg), envMap(nil), testDirs)
			require.NoError(t, err)
			assert.Equal(t, map[string]string{item.key: "--" + item.flag}, cfg.Overrides, "flag")

			cfg, err = ResolveSettings(newFlags(t), env, testDirs)
			require.NoError(t, err)
			assert.Equal(t, map[string]string{item.key: item.env}, cfg.Overrides, "environment")

			cfg, err = ResolveSettings(newFlags(t, flagArg), env, testDirs)
			require.NoError(t, err)
			assert.Equal(t, map[string]string{item.key: "--" + item.flag}, cfg.Overrides, "the flag wins")
		})
	}
}

// TestResolveSettingsOverridesSkipFileValues leaves keys set only by the
// settings file out of the overrides, since the dialog may change them.
func TestResolveSettingsOverridesSkipFileValues(t *testing.T) {
	t.Parallel()

	dirs := fileDirs(t, "mode: cache-only\ninterval: 10m\nidleInterval: 1h\nnotify: [50]\nlogLevel: warn\n")

	cfg, err := ResolveSettings(newFlags(t, "--mode", "hybrid"),
		envMap(map[string]string{config.EnvInterval: "7m", config.EnvNotify: "60"}), dirs)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		config.KeyMode:     "--mode",
		config.KeyInterval: config.EnvInterval,
		config.KeyNotify:   config.EnvNotify,
	}, cfg.Overrides)
	assert.Equal(t, time.Hour, cfg.IdleInterval, "the file value applies without an override")
	assert.Equal(t, slog.LevelWarn, cfg.LogLevel, "the file value applies without an override")
}

// TestResolveSettingsWithoutLookupEnv checks a nil reader means flags only.
func TestResolveSettingsWithoutLookupEnv(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveSettings(newFlags(t, "--interval", "3m"), nil, testDirs)
	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, cfg.Interval)
	assert.Equal(t, config.ModeHybrid, cfg.Mode)
}

// TestResolveSettingsPrecedence checks flag beats environment beats default
// for every setting.
//
// The systemd unit sets the environment and a developer overrides it on the
// command line, so the order is part of the interface.
func TestResolveSettingsPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fromFlg any
		fromEnv any
		fromDef any
		get     func(cfg config.Config) any
		name    string
		flag    string
		env     string
		flagVal string
		envVal  string
	}{
		{
			name: "mode", flag: flagMode, env: config.EnvMode,
			flagVal: "hybrid", envVal: "cache-only",
			fromFlg: config.ModeHybrid, fromEnv: config.ModeCacheOnly, fromDef: config.ModeHybrid,
			get: func(cfg config.Config) any { return cfg.Mode },
		},
		{
			name: "interval", flag: flagInterval, env: config.EnvInterval,
			flagVal: "3m", envVal: "4m",
			fromFlg: 3 * time.Minute, fromEnv: 4 * time.Minute, fromDef: config.DefaultInterval,
			get: func(cfg config.Config) any { return cfg.Interval },
		},
		{
			name: "idle interval", flag: flagIdleInterval, env: config.EnvIdleInterval,
			flagVal: "30m", envVal: "40m",
			fromFlg: 30 * time.Minute, fromEnv: 40 * time.Minute, fromDef: config.DefaultIdleInterval,
			get: func(cfg config.Config) any { return cfg.IdleInterval },
		},
		{
			name: "notify", flag: flagNotify, env: config.EnvNotify,
			flagVal: "50", envVal: "70,60",
			fromFlg: []float64{50}, fromEnv: []float64{60, 70}, fromDef: config.DefaultThresholds(),
			get: func(cfg config.Config) any { return cfg.Thresholds },
		},
		{
			name: "notify auth", flag: flagNotifyAuth, env: config.EnvNotifyAuth,
			flagVal: "false", envVal: "0",
			fromFlg: false, fromEnv: false, fromDef: true,
			get: func(cfg config.Config) any { return cfg.NotifyAuth },
		},
		{
			name: "log level", flag: flagLogLevel, env: config.EnvLogLevel,
			flagVal: "debug", envVal: "warn",
			fromFlg: slog.LevelDebug, fromEnv: slog.LevelWarn, fromDef: slog.LevelInfo,
			get: func(cfg config.Config) any { return cfg.LogLevel },
		},
		{
			name: "claude config dir", flag: flagClaudeConfigDir, env: config.EnvClaudeConfigDir,
			flagVal: "/flag/claude", envVal: "/env/claude",
			fromFlg: "/flag/claude", fromEnv: "/env/claude", fromDef: "",
			get: func(cfg config.Config) any { return cfg.ClaudeDir },
		},
		{
			name: "state dir", flag: flagStateDir, env: config.EnvStateDir,
			flagVal: "/flag/state", envVal: "/env/state",
			fromFlg: "/flag/state", fromEnv: "/env/state",
			fromDef: filepath.Join(testDirs.StateHome, "clankerwatch"),
			get:     func(cfg config.Config) any { return cfg.StateDir },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := envMap(map[string]string{tt.env: tt.envVal})

			cfg, err := ResolveSettings(newFlags(t, "--"+tt.flag+"="+tt.flagVal), env, testDirs)
			require.NoError(t, err)
			assert.Equal(t, tt.fromFlg, tt.get(cfg), "flag beats environment")

			cfg, err = ResolveSettings(newFlags(t), env, testDirs)
			require.NoError(t, err)
			assert.Equal(t, tt.fromEnv, tt.get(cfg), "environment beats default")

			cfg, err = ResolveSettings(newFlags(t), envMap(nil), testDirs)
			require.NoError(t, err)
			assert.Equal(t, tt.fromDef, tt.get(cfg), "default")
		})
	}
}

// TestResolveSettingsEmptyStateDirKeepsDefault checks an empty variable is
// ignored.
//
// systemd's EnvironmentFile turns "CLANKERWATCH_STATE_DIR=" into an empty
// value, which must not move the state to the working directory.
func TestResolveSettingsEmptyStateDirKeepsDefault(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveSettings(newFlags(t), envMap(map[string]string{config.EnvStateDir: ""}), testDirs)
	require.NoError(t, err)
	assert.Equal(t, testDirs.AppStateDir(), cfg.StateDir)
}

// TestResolveSettingsEmptyNotifyTurnsAlertsOff checks the documented way to
// silence threshold alerts.
func TestResolveSettingsEmptyNotifyTurnsAlertsOff(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveSettings(newFlags(t, "--notify="), envMap(nil), testDirs)
	require.NoError(t, err)
	assert.Empty(t, cfg.Thresholds)
}

// TestResolveSettingsInvalid checks each unusable value per setting.
//
// Every failure must wrap ErrInvalidSetting, which the app maps to exit code
// 2, and name where the value came from.
func TestResolveSettingsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env    map[string]string
		name   string
		source string
		dirs   config.Dirs
		args   []string
	}{
		{name: "mode flag", args: []string{"--mode", "bogus"}, source: "--mode"},
		{name: "mode env", env: map[string]string{config.EnvMode: "bogus"}, source: config.EnvMode},
		{name: "interval unparsable", env: map[string]string{config.EnvInterval: "soon"}, source: config.EnvInterval},
		{name: "interval too short", args: []string{"--interval", "1m"}, source: "interval"},
		{
			name: "idle interval unparsable", env: map[string]string{config.EnvIdleInterval: "later"},
			source: config.EnvIdleInterval,
		},
		{name: "idle interval below interval", args: []string{"--idle-interval", "3m"}, source: "idle interval"},
		{name: "notify flag", args: []string{"--notify", "abc"}, source: "--notify"},
		{name: "notify zero", env: map[string]string{config.EnvNotify: "0"}, source: config.EnvNotify},
		{name: "notify above 100", env: map[string]string{config.EnvNotify: "101"}, source: config.EnvNotify},
		{name: "notify auth", env: map[string]string{config.EnvNotifyAuth: "maybe"}, source: config.EnvNotifyAuth},
		{name: "log level flag", args: []string{"--log-level", "loud"}, source: "--log-level"},
		{name: "log level env", env: map[string]string{config.EnvLogLevel: "loud"}, source: config.EnvLogLevel},
		{name: "no home", dirs: config.Dirs{Home: "", StateHome: "/state"}, source: "home"},
		{name: "no state dir", dirs: config.Dirs{Home: "/home/tester", StateHome: ""}, source: "state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dirs := tt.dirs
			if dirs == (config.Dirs{}) {
				dirs = testDirs
			}

			cfg, err := ResolveSettings(newFlags(t, tt.args...), envMap(tt.env), dirs)
			require.ErrorIs(t, err, ErrInvalidSetting)
			assert.Contains(t, err.Error(), tt.source)
			assert.Equal(t, config.Config{}, cfg)
		})
	}
}

// TestResolveSettingsFlagShadowsInvalidEnv checks a flag rescues a bad
// variable.
//
// The environment is only read when the flag is absent, so a broken env file
// can be overridden on the command line.
func TestResolveSettingsFlagShadowsInvalidEnv(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveSettings(newFlags(t, "--mode", "cache-only"),
		envMap(map[string]string{config.EnvMode: "bogus"}), testDirs)
	require.NoError(t, err)
	assert.Equal(t, config.ModeCacheOnly, cfg.Mode)
}

// fileDirs returns XDG directories in a temporary directory, with content
// written to the default settings file when it is not empty.
//
// Parameters:
//   - t: test handle.
//   - content: the settings file content, or empty for no file.
//
// Returns:
//   - config.Dirs: directories whose ConfigHome holds the settings file.
func fileDirs(t *testing.T, content string) config.Dirs {
	t.Helper()

	root := t.TempDir()
	dirs := config.Dirs{
		Home:       filepath.Join(root, "home"),
		StateHome:  filepath.Join(root, "state"),
		ConfigHome: filepath.Join(root, "config"),
	}

	if content != "" {
		writeFile(t, dirs.AppSettingsFile(), content)
	}

	return dirs
}

// writeFile writes content to path, creating its directory.
//
// Parameters:
//   - t: test handle.
//   - path: the file to write.
//   - content: the file content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// TestResolveSettingsLayers checks the settings file sits beneath the
// environment, which sits beneath the flags.
func TestResolveSettingsLayers(t *testing.T) {
	t.Parallel()

	dirs := fileDirs(t, "mode: cache-only\ninterval: 10m\nidleInterval: 1h\nnotify: [50]\n")

	cfg, err := ResolveSettings(newFlags(t, "--mode", "hybrid"),
		envMap(map[string]string{config.EnvInterval: "7m"}), dirs)
	require.NoError(t, err)

	assert.Equal(t, config.ModeHybrid, cfg.Mode, "the flag beats the file")
	assert.Equal(t, 7*time.Minute, cfg.Interval, "the environment beats the file")
	assert.Equal(t, time.Hour, cfg.IdleInterval, "the file beats the default")
	assert.Equal(t, []float64{50}, cfg.Thresholds, "the file beats the default")
	assert.Equal(t, dirs.AppSettingsFile(), cfg.SettingsFile)
}

// TestResolveSettingsMissingDefaultFile uses the defaults when the default
// settings file does not exist and still reports where it would be.
func TestResolveSettingsMissingDefaultFile(t *testing.T) {
	t.Parallel()

	dirs := fileDirs(t, "")

	cfg, err := ResolveSettings(newFlags(t), envMap(nil), dirs)
	require.NoError(t, err)

	want := config.Default(dirs)
	want.SettingsFile = dirs.AppSettingsFile()
	assert.Equal(t, want, cfg)
}

// TestResolveSettingsNamedFile reads the file --config or the environment
// names in place of the default one, the flag first.
func TestResolveSettingsNamedFile(t *testing.T) {
	t.Parallel()

	dirs := fileDirs(t, "interval: 3m\n")
	fromEnv := filepath.Join(t.TempDir(), "env.yaml")
	fromFlag := filepath.Join(t.TempDir(), "flag.yaml")

	writeFile(t, fromEnv, "interval: 4m\n")
	writeFile(t, fromFlag, "interval: 6m\n")

	env := envMap(map[string]string{config.EnvConfig: fromEnv})

	cfg, err := ResolveSettings(newFlags(t), env, dirs)
	require.NoError(t, err)
	assert.Equal(t, 4*time.Minute, cfg.Interval)
	assert.Equal(t, fromEnv, cfg.SettingsFile)

	cfg, err = ResolveSettings(newFlags(t, "--config", fromFlag), env, dirs)
	require.NoError(t, err)
	assert.Equal(t, 6*time.Minute, cfg.Interval)
	assert.Equal(t, fromFlag, cfg.SettingsFile)
}

// TestResolveSettingsEmptyConfigUsesDefaultFile treats an empty --config or
// variable as unset.
func TestResolveSettingsEmptyConfigUsesDefaultFile(t *testing.T) {
	t.Parallel()

	dirs := fileDirs(t, "interval: 3m\n")

	cfg, err := ResolveSettings(newFlags(t, "--config", ""),
		envMap(map[string]string{config.EnvConfig: ""}), dirs)
	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, cfg.Interval)
	assert.Equal(t, dirs.AppSettingsFile(), cfg.SettingsFile)
}

// TestResolveSettingsNamedFileMustExist refuses a missing file the user named.
func TestResolveSettingsNamedFileMustExist(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := ResolveSettings(newFlags(t, "--config", missing), envMap(nil), fileDirs(t, ""))
	require.ErrorIs(t, err, ErrInvalidSetting)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestResolveSettingsBadFile reports undecodable files and unusable values.
//
// Decode and value errors name the file. Validation runs after every layer
// is applied, so its errors name only the setting.
func TestResolveSettingsBadFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want     error
		name     string
		content  string
		namePath bool
	}{
		{name: "unknown key", content: "polling: 5m\n", want: config.ErrInvalidFile, namePath: true},
		{name: "bad mode", content: "mode: online\n", want: config.ErrInvalidMode, namePath: true},
		{name: "bad threshold", content: "notify: [0]\n", want: config.ErrInvalidThreshold, namePath: true},
		{name: "bad log level", content: "logLevel: loud\n", want: config.ErrInvalidLogLevel, namePath: true},
		{name: "short interval", content: "interval: 1m\n", want: config.ErrIntervalTooShort, namePath: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dirs := fileDirs(t, test.content)

			_, err := ResolveSettings(newFlags(t), envMap(nil), dirs)
			require.ErrorIs(t, err, ErrInvalidSetting)
			require.ErrorIs(t, err, test.want)

			if test.namePath {
				assert.Contains(t, err.Error(), dirs.AppSettingsFile())
			}
		})
	}
}

// TestResolveSettingsFlagRepairsFileValue lets a flag replace a file value
// that would fail validation, the same as with an environment value.
func TestResolveSettingsFlagRepairsFileValue(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveSettings(newFlags(t, "--interval", "3m"), envMap(nil), fileDirs(t, "interval: 1m\n"))
	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, cfg.Interval)
}

// newFlags returns parsed persistent flags as the root command binds them.
func newFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()

	root := &cobra.Command{Use: Name}
	BindPersistent(root, New())

	err := root.PersistentFlags().Parse(args)
	require.NoError(t, err)

	return root.PersistentFlags()
}

// envMap returns an environment reader backed by values.
func envMap(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]

		return value, ok
	}
}

// TestResolveSettingsEmptyStateDirIsNoOverride keeps an empty state directory
// out of the overrides.
//
// An empty value keeps the directory beneath it, so the settings file's
// stateDir still applies and must not be reported as held by the environment.
func TestResolveSettingsEmptyStateDirIsNoOverride(t *testing.T) {
	t.Parallel()

	dirs := fileDirs(t, "stateDir: /from-file\n")

	cfg, err := ResolveSettings(newFlags(t), envMap(map[string]string{config.EnvStateDir: ""}), dirs)
	require.NoError(t, err)
	assert.Equal(t, "/from-file", cfg.StateDir)
	assert.NotContains(t, cfg.Overrides, config.KeyStateDir)
}
