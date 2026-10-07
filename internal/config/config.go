// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Mode selects where usage data comes from.
type Mode string

// Config holds the resolved daemon settings.
type Config struct {
	// Mode selects the data source.
	Mode Mode

	// ClaudeDir is CLAUDE_CONFIG_DIR, or empty for Claude Code's default.
	ClaudeDir string

	// Home is the user's home directory.
	Home string

	// StateDir is the directory that holds the daemon's state file.
	StateDir string

	// SettingsFile is the YAML settings file path. The file may not exist.
	SettingsFile string

	// Overrides maps each settings file key that an environment variable or
	// flag sets to that source, such as CLANKERWATCH_INTERVAL or --interval.
	// The settings file cannot change those keys.
	Overrides map[string]string

	// Thresholds are the alert thresholds in percent, ascending. Empty
	// disables threshold alerts.
	Thresholds []float64

	// Interval is the polling interval while Claude Code is active.
	Interval time.Duration

	// IdleInterval is the polling interval while Claude Code is idle.
	IdleInterval time.Duration

	// LogLevel is the minimum level written to the log.
	LogLevel slog.Level

	// NotifyAuth enables the alert for a login that needs attention.
	NotifyAuth bool
}

const (
	// ModeHybrid polls the usage endpoint with Claude Code's token and adopts
	// Claude Code's own cached result when it is fresher.
	ModeHybrid Mode = "hybrid"

	// ModeCacheOnly never reads credentials or uses the network.
	ModeCacheOnly Mode = "cache-only"

	// MinInterval is the shortest allowed polling interval. The usage endpoint
	// allows only a handful of requests every few minutes.
	MinInterval = 2 * time.Minute

	// DefaultInterval is the polling interval while Claude Code is active.
	DefaultInterval = 5 * time.Minute

	// DefaultIdleInterval is the polling interval while Claude Code is idle.
	DefaultIdleInterval = 20 * time.Minute

	// defaultWarn is the warning color threshold when alerts are disabled.
	defaultWarn = 80.0

	// defaultCrit is the critical color threshold when alerts are disabled.
	defaultCrit = 95.0

	// maxPercent is the largest valid threshold.
	maxPercent = 100.0

	// stateFileName is the name of the state file inside StateDir.
	stateFileName = "state.json"

	// appDirName is the application's directory name under XDG directories.
	appDirName = "clankerwatch"

	// settingsFileName is the settings file's name inside the config directory.
	settingsFileName = "config.yaml"
)

var (
	// ErrInvalidMode indicates a mode other than hybrid or cache-only.
	ErrInvalidMode = errors.New("invalid mode")

	// ErrIntervalTooShort indicates a polling interval below MinInterval.
	ErrIntervalTooShort = errors.New("interval is below the minimum")

	// ErrIdleTooShort indicates an idle interval shorter than the interval.
	ErrIdleTooShort = errors.New("idle interval is shorter than the interval")

	// ErrInvalidThreshold indicates an alert threshold outside (0, 100].
	ErrInvalidThreshold = errors.New("invalid alert threshold")

	// ErrInvalidLogLevel indicates a log level other than debug, info, warn, or error.
	ErrInvalidLogLevel = errors.New("invalid log level")

	// ErrNoHome indicates that neither a home directory nor CLAUDE_CONFIG_DIR is known.
	ErrNoHome = errors.New("home directory is not set")

	// ErrNoStateDir indicates that no state directory is known.
	ErrNoStateDir = errors.New("state directory is not set")
)

// DefaultThresholds returns the default alert thresholds.
//
// Returns:
//   - []float64: a fresh slice of 80 and 95.
func DefaultThresholds() []float64 {
	return []float64{defaultWarn, defaultCrit}
}

// Default returns the settings used when nothing overrides them.
//
// Parameters:
//   - dirs: the user's XDG directories.
//
// Returns:
//   - Config: default settings rooted in dirs.
func Default(dirs Dirs) Config {
	return Config{
		Mode:         ModeHybrid,
		Interval:     DefaultInterval,
		IdleInterval: DefaultIdleInterval,
		Thresholds:   DefaultThresholds(),
		NotifyAuth:   true,
		ClaudeDir:    "",
		Home:         dirs.Home,
		StateDir:     dirs.AppStateDir(),
		SettingsFile: "",
		Overrides:    map[string]string{},
		LogLevel:     slog.LevelInfo,
	}
}

// ParseMode validates a mode name.
//
// Parameters:
//   - value: hybrid or cache-only.
//
// Returns:
//   - Mode: the parsed mode.
//   - error: ErrInvalidMode for any other value.
func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.TrimSpace(value))
	if mode != ModeHybrid && mode != ModeCacheOnly {
		return "", fmt.Errorf("%w %q: want %q or %q", ErrInvalidMode, value, ModeHybrid, ModeCacheOnly)
	}

	return mode, nil
}

// NormalizeThresholds validates alert thresholds and sorts them.
//
// Duplicates are dropped and the result is sorted ascending. An empty list
// disables threshold alerts.
//
// Parameters:
//   - thresholds: thresholds in percent, in any order.
//
// Returns:
//   - []float64: a new ascending, unique list in (0, 100].
//   - error: ErrInvalidThreshold for a value outside the range.
func NormalizeThresholds(thresholds []float64) ([]float64, error) {
	for _, threshold := range thresholds {
		if !inPercentRange(threshold) {
			return nil, fmt.Errorf("%w %v: want a number in (0, 100]", ErrInvalidThreshold, threshold)
		}
	}

	sorted := slices.Clone(thresholds)
	slices.Sort(sorted)

	return slices.Compact(sorted), nil
}

// ParseLogLevel parses a log level name.
//
// Parameters:
//   - value: debug, info, warn, or error, in any case.
//
// Returns:
//   - [slog.Level]: the level.
//   - error: ErrInvalidLogLevel for any other value.
func ParseLogLevel(value string) (slog.Level, error) {
	var level slog.Level

	err := level.UnmarshalText([]byte(value))
	if err != nil {
		return 0, fmt.Errorf("%w %q: %w", ErrInvalidLogLevel, value, err)
	}

	return level, nil
}

// ParseThresholds parses a comma-separated list of alert thresholds.
//
// Blank entries are skipped, duplicates are dropped, and the result is
// sorted ascending. An empty value disables threshold alerts.
//
// Parameters:
//   - value: thresholds such as "80,95".
//
// Returns:
//   - []float64: ascending, unique thresholds in (0, 100].
//   - error: ErrInvalidThreshold for a value that is not a number in range.
func ParseThresholds(value string) ([]float64, error) {
	var thresholds []float64

	for field := range strings.SplitSeq(value, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		threshold, err := strconv.ParseFloat(field, 64)
		if err != nil || !inPercentRange(threshold) {
			return nil, fmt.Errorf("%w %q: want a number in (0, 100]", ErrInvalidThreshold, field)
		}

		thresholds = append(thresholds, threshold)
	}

	return NormalizeThresholds(thresholds)
}

// ColorThresholds returns the percentages at which bars turn to warning and
// critical colors.
//
// They follow the alert thresholds so colors and alerts agree. Without
// alerts, the colors use 80 and 95.
//
// Returns:
//   - warn: the warning threshold.
//   - crit: the critical threshold.
//
//nolint:nonamedreturns // Same-type returns need names.
func (c Config) ColorThresholds() (warn, crit float64) {
	if len(c.Thresholds) == 0 {
		return defaultWarn, defaultCrit
	}

	warn, crit = c.Thresholds[0], c.Thresholds[len(c.Thresholds)-1]

	return warn, crit
}

// StateFile returns the path of the daemon's state file.
//
// Returns:
//   - string: state.json inside StateDir.
func (c Config) StateFile() string {
	return filepath.Join(c.StateDir, stateFileName)
}

// Validate reports the first setting that cannot work.
//
// Returns:
//   - error: a wrapped sentinel error, or nil when the settings are usable.
func (c Config) Validate() error {
	_, err := ParseMode(string(c.Mode))
	if err != nil {
		return err
	}

	if c.Interval < MinInterval {
		return fmt.Errorf("%w: %v < %v", ErrIntervalTooShort, c.Interval, MinInterval)
	}

	if c.IdleInterval < c.Interval {
		return fmt.Errorf("%w: %v < %v", ErrIdleTooShort, c.IdleInterval, c.Interval)
	}

	for _, threshold := range c.Thresholds {
		if !inPercentRange(threshold) {
			return fmt.Errorf("%w: %v", ErrInvalidThreshold, threshold)
		}
	}

	if c.Home == "" && c.ClaudeDir == "" {
		return ErrNoHome
	}

	if c.StateDir == "" {
		return ErrNoStateDir
	}

	return nil
}

// inPercentRange reports whether a threshold is a number in (0, 100].
//
// Parameters:
//   - threshold: the value to check.
//
// Returns:
//   - bool: false for NaN and for values outside the range.
func inPercentRange(threshold float64) bool {
	return threshold > 0 && threshold <= maxPercent
}
