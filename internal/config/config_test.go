// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"log/slog"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validConfig returns settings that pass Validate.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - Config: defaults rooted in a temporary directory.
func validConfig(t *testing.T) Config {
	t.Helper()

	root := t.TempDir()

	return Default(Dirs{Home: root, StateHome: filepath.Join(root, "state")})
}

// TestDefaultThresholds returns 80 and 95 in a fresh slice each call.
//
// A shared slice would let one Config's edits leak into every later default.
func TestDefaultThresholds(t *testing.T) {
	t.Parallel()

	first := DefaultThresholds()
	assert.Equal(t, []float64{80, 95}, first)

	first[0] = 1

	assert.Equal(t, []float64{80, 95}, DefaultThresholds())
}

// TestDefault fills every setting and roots the state in the given dirs.
func TestDefault(t *testing.T) {
	t.Parallel()

	dirs := Dirs{Home: "/home/user", StateHome: "/home/user/.local/state"}
	got := Default(dirs)

	assert.Equal(t, ModeHybrid, got.Mode)
	assert.Equal(t, DefaultInterval, got.Interval)
	assert.Equal(t, DefaultIdleInterval, got.IdleInterval)
	assert.Equal(t, DefaultThresholds(), got.Thresholds)
	assert.True(t, got.NotifyAuth)
	assert.Empty(t, got.ClaudeDir)
	assert.Equal(t, "/home/user", got.Home)
	assert.Equal(t, filepath.Join("/home/user/.local/state", "clankerwatch"), got.StateDir)
	assert.Equal(t, slog.LevelInfo, got.LogLevel)
	require.NotNil(t, got.Overrides, "callers record overrides without allocating")
	assert.Empty(t, got.Overrides)
	require.NoError(t, got.Validate())

	got.Overrides[KeyInterval] = EnvInterval

	assert.Empty(t, Default(dirs).Overrides, "each default has its own map")
}

// TestDefaultIntervalsRespectTheFloor keeps the defaults above MinInterval.
func TestDefaultIntervalsRespectTheFloor(t *testing.T) {
	t.Parallel()

	assert.GreaterOrEqual(t, DefaultInterval, MinInterval)
	assert.GreaterOrEqual(t, DefaultIdleInterval, DefaultInterval)
}

// TestParseMode accepts the two modes, trimmed, and nothing else.
func TestParseMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    Mode
		wantErr bool
	}{
		{name: "hybrid", value: "hybrid", want: ModeHybrid},
		{name: "cache-only", value: "cache-only", want: ModeCacheOnly},
		{name: "padded", value: "  cache-only\n", want: ModeCacheOnly},
		{name: "empty", value: "", wantErr: true},
		{name: "wrong case", value: "Hybrid", wantErr: true},
		{name: "underscore", value: "cache_only", wantErr: true},
		{name: "unknown", value: "network", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseMode(tt.value)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidMode)
				require.ErrorContains(t, err, `"hybrid" or "cache-only"`)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParseThresholds covers parsing, ordering, and de-duplication.
func TestParseThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  []float64
	}{
		{name: "empty disables alerts", value: "", want: nil},
		{name: "blanks only", value: " , ,", want: nil},
		{name: "defaults", value: "80,95", want: []float64{80, 95}},
		{name: "sorted", value: "95,50,80", want: []float64{50, 80, 95}},
		{name: "deduplicated", value: "90, 90 ,80,90", want: []float64{80, 90}},
		{name: "blank entries skipped", value: ",,50,", want: []float64{50}},
		{name: "upper bound", value: "100", want: []float64{100}},
		{name: "fractions", value: "0.5,99.9", want: []float64{0.5, 99.9}},
		{name: "equal spellings merge", value: "80,80.0,8e1", want: []float64{80}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseThresholds(tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParseThresholdsRejects returns ErrInvalidThreshold for each bad entry.
func TestParseThresholdsRejects(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "-1", "100.01", "101", "abc", "80,x", "Inf", "-Inf", "80;95", "1e3"} {
		got, err := ParseThresholds(value)
		require.ErrorIs(t, err, ErrInvalidThreshold, "value %q", value)
		assert.Nil(t, got, "value %q", value)
	}
}

// TestParseThresholdsRejectsNaN keeps NaN out of the thresholds.
//
// strconv.ParseFloat accepts "NaN", and NaN fails every comparison. A NaN
// threshold would never compare as reached, so its alert could never fire,
// and it would sort first and become the warning color threshold.
func TestParseThresholdsRejectsNaN(t *testing.T) {
	t.Parallel()

	_, err := ParseThresholds("NaN")
	require.ErrorIs(t, err, ErrInvalidThreshold)
}

// TestColorThresholds follows the alert thresholds or falls back to 80 and 95.
func TestColorThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		thresholds []float64
		wantWarn   float64
		wantCrit   float64
	}{
		{name: "none", thresholds: nil, wantWarn: 80, wantCrit: 95},
		{name: "empty", thresholds: []float64{}, wantWarn: 80, wantCrit: 95},
		{name: "one", thresholds: []float64{70}, wantWarn: 70, wantCrit: 70},
		{name: "two", thresholds: []float64{60, 90}, wantWarn: 60, wantCrit: 90},
		{name: "three", thresholds: []float64{50, 75, 99}, wantWarn: 50, wantCrit: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			warn, crit := Config{Thresholds: tt.thresholds}.ColorThresholds()
			assert.InDelta(t, tt.wantWarn, warn, 1e-9)
			assert.InDelta(t, tt.wantCrit, crit, 1e-9)
		})
	}
}

// TestStateFile joins the state file name onto StateDir.
func TestStateFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.Equal(t, filepath.Join(dir, "state.json"), Config{StateDir: dir}.StateFile())
}

// TestValidate reports the first setting that cannot work.
//
// The interval floor is the important one: a daemon that polls faster than the
// endpoint allows would be rate limited for everyone sharing the token.
func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target error
		mutate func(*Config)
		name   string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "cache-only", mutate: func(c *Config) { c.Mode = ModeCacheOnly }},
		{name: "no thresholds", mutate: func(c *Config) { c.Thresholds = nil }},
		{name: "interval at the floor", mutate: func(c *Config) { c.Interval, c.IdleInterval = MinInterval, MinInterval }},
		{name: "claude dir without home", mutate: func(c *Config) { c.Home, c.ClaudeDir = "", "/opt/claude" }},
		{name: "bad mode", target: ErrInvalidMode, mutate: func(c *Config) { c.Mode = "network" }},
		{name: "empty mode", target: ErrInvalidMode, mutate: func(c *Config) { c.Mode = "" }},
		{
			name: "interval below floor", target: ErrIntervalTooShort,
			mutate: func(c *Config) { c.Interval = MinInterval - time.Second },
		},
		{
			name: "idle shorter than interval", target: ErrIdleTooShort,
			mutate: func(c *Config) { c.IdleInterval = c.Interval - time.Second },
		},
		{name: "zero threshold", target: ErrInvalidThreshold, mutate: func(c *Config) { c.Thresholds = []float64{0} }},
		{
			name: "threshold above range", target: ErrInvalidThreshold,
			mutate: func(c *Config) { c.Thresholds = []float64{80, 100.5} },
		},
		{
			name: "infinite threshold", target: ErrInvalidThreshold,
			mutate: func(c *Config) { c.Thresholds = []float64{math.Inf(1)} },
		},
		{name: "no home", target: ErrNoHome, mutate: func(c *Config) { c.Home = "" }},
		{name: "no state dir", target: ErrNoStateDir, mutate: func(c *Config) { c.StateDir = "" }},
		{
			name: "mode checked first", target: ErrInvalidMode,
			mutate: func(c *Config) { c.Mode, c.Interval, c.StateDir = "x", 0, "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig(t)
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.target == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.target)
		})
	}
}

// TestValidateRejectsNaNThreshold keeps a NaN threshold from validating.
//
// A Config built in code could carry NaN without going through
// ParseThresholds.
func TestValidateRejectsNaNThreshold(t *testing.T) {
	t.Parallel()

	cfg := validConfig(t)
	cfg.Thresholds = []float64{math.NaN()}

	require.ErrorIs(t, cfg.Validate(), ErrInvalidThreshold)
}

// TestNormalizeThresholds sorts, deduplicates, and range-checks a list.
func TestNormalizeThresholds(t *testing.T) {
	t.Parallel()

	input := []float64{95, 50, 95, 100}

	got, err := NormalizeThresholds(input)
	require.NoError(t, err)
	assert.Equal(t, []float64{50, 95, 100}, got)
	assert.Equal(t, []float64{95, 50, 95, 100}, input, "the input is not modified")

	got, err = NormalizeThresholds(nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	for _, bad := range []float64{0, -1, 100.5, math.NaN(), math.Inf(1)} {
		_, err = NormalizeThresholds([]float64{80, bad})
		require.ErrorIs(t, err, ErrInvalidThreshold, "%v", bad)
	}
}

// TestParseLogLevel accepts slog's level names in any case.
func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	tests := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"INFO":  slog.LevelInfo,
		"Warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}

	for value, want := range tests {
		got, err := ParseLogLevel(value)
		require.NoError(t, err, value)
		assert.Equal(t, want, got, value)
	}

	_, err := ParseLogLevel("loud")
	require.ErrorIs(t, err, ErrInvalidLogLevel)
}
