// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package options

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// Options holds the settings flag values for one command execution.
type Options struct {
	// Config is the --config value.
	Config string

	// Mode is the --mode value.
	Mode string

	// Notify is the --notify value.
	Notify string

	// LogLevel is the --log-level value.
	LogLevel string

	// ClaudeConfigDir is the --claude-config-dir value.
	ClaudeConfigDir string

	// StateDir is the --state-dir value.
	StateDir string

	// Interval is the --interval value.
	Interval time.Duration

	// IdleInterval is the --idle-interval value.
	IdleInterval time.Duration

	// NotifyAuth is the --notify-auth value.
	NotifyAuth bool
}

// Resolve turns flags, environment, the settings file, and defaults into
// validated settings.
type Resolve func() (config.Config, error)

// LookupEnv reads an environment variable, like [os.LookupEnv].
type LookupEnv = func(key string) (string, bool)

// setting is one flag, its environment variable, its settings file key, and
// how to apply a value.
type setting struct {
	apply func(cfg *config.Config, value string) error
	flag  string
	env   string
	key   string
}

const (
	// Name is the program name.
	Name = "clankerwatch"
)

// Flag names.
const (
	flagConfig          = "config"
	flagMode            = "mode"
	flagInterval        = "interval"
	flagIdleInterval    = "idle-interval"
	flagNotify          = "notify"
	flagNotifyAuth      = "notify-auth"
	flagLogLevel        = "log-level"
	flagClaudeConfigDir = "claude-config-dir"
	flagStateDir        = "state-dir"
)

// ErrInvalidSetting wraps every flag, environment, or settings file value that
// cannot be used.
var ErrInvalidSetting = errors.New("invalid setting")

// settings lists every setting in resolution order.
var settings = []setting{
	{apply: applyMode, flag: flagMode, env: config.EnvMode, key: config.KeyMode},
	{apply: applyInterval, flag: flagInterval, env: config.EnvInterval, key: config.KeyInterval},
	{apply: applyIdleInterval, flag: flagIdleInterval, env: config.EnvIdleInterval, key: config.KeyIdleInterval},
	{apply: applyNotify, flag: flagNotify, env: config.EnvNotify, key: config.KeyNotify},
	{apply: applyNotifyAuth, flag: flagNotifyAuth, env: config.EnvNotifyAuth, key: config.KeyNotifyAuth},
	{apply: applyLogLevel, flag: flagLogLevel, env: config.EnvLogLevel, key: config.KeyLogLevel},
	{
		apply: applyClaudeConfigDir, flag: flagClaudeConfigDir,
		env: config.EnvClaudeConfigDir, key: config.KeyClaudeConfigDir,
	},
	{apply: applyStateDir, flag: flagStateDir, env: config.EnvStateDir, key: config.KeyStateDir},
}

// New returns flag values before cobra parses arguments.
//
// Returns:
//   - *Options: values matching the defaults in config.
func New() *Options {
	return &Options{
		Config:          "",
		Mode:            string(config.ModeHybrid),
		Notify:          "80,95",
		LogLevel:        "info",
		ClaudeConfigDir: "",
		StateDir:        "",
		Interval:        config.DefaultInterval,
		IdleInterval:    config.DefaultIdleInterval,
		NotifyAuth:      true,
	}
}

// BindPersistent registers the settings flags on the root command.
//
// Parameters:
//   - command: the root command.
//   - opts: destination for the flag values.
func BindPersistent(command *cobra.Command, opts *Options) {
	flags := command.PersistentFlags()
	flags.StringVar(&opts.Config, flagConfig, opts.Config,
		"YAML settings file, default $XDG_CONFIG_HOME/clankerwatch/config.yaml ($"+config.EnvConfig+")")
	flags.StringVar(&opts.Mode, flagMode, opts.Mode,
		"data source: hybrid, or cache-only to never read credentials or use the network ($"+config.EnvMode+")")
	flags.DurationVar(&opts.Interval, flagInterval, opts.Interval,
		"polling interval while Claude Code is active, at least 2m ($"+config.EnvInterval+")")
	flags.DurationVar(&opts.IdleInterval, flagIdleInterval, opts.IdleInterval,
		"polling interval while Claude Code is idle ($"+config.EnvIdleInterval+")")
	flags.StringVar(&opts.Notify, flagNotify, opts.Notify,
		"alert thresholds in percent, empty to turn alerts off ($"+config.EnvNotify+")")
	flags.BoolVar(&opts.NotifyAuth, flagNotifyAuth, opts.NotifyAuth,
		"alert when the Claude Code login needs attention ($"+config.EnvNotifyAuth+")")
	flags.StringVar(&opts.LogLevel, flagLogLevel, opts.LogLevel,
		"debug, info, warn, or error ($"+config.EnvLogLevel+")")
	flags.StringVar(&opts.ClaudeConfigDir, flagClaudeConfigDir, opts.ClaudeConfigDir,
		"Claude Code's config directory ($"+config.EnvClaudeConfigDir+")")
	flags.StringVar(&opts.StateDir, flagStateDir, opts.StateDir,
		"directory for the daemon's state, default $XDG_STATE_HOME/clankerwatch ($"+config.EnvStateDir+")")
}

// ResolveSettings applies the settings file, then environment variables, then
// given flags over the defaults and validates the result.
//
// A settings file named by --config or the environment must exist. The
// default file is optional.
//
// Parameters:
//   - flags: the parsed persistent flags.
//   - lookupEnv: reads an environment variable.
//   - dirs: the user's XDG directories.
//
// Returns:
//   - config.Config: the validated settings.
//   - error: an error wrapping ErrInvalidSetting.
func ResolveSettings(flags *pflag.FlagSet, lookupEnv LookupEnv, dirs config.Dirs) (config.Config, error) {
	cfg := config.Default(dirs)

	err := applyFile(&cfg, flags, lookupEnv, dirs)
	if err != nil {
		return config.Config{}, err
	}

	for _, item := range settings {
		value, source, ok := pick(flags, lookupEnv, item)
		if !ok {
			continue
		}

		err = item.apply(&cfg, value)
		if err != nil {
			return config.Config{}, fmt.Errorf("%w: %s: %w", ErrInvalidSetting, source, err)
		}

		// An empty state directory keeps the one beneath it, so it holds
		// nothing for the settings file to defer to.
		if item.key != config.KeyStateDir || value != "" {
			cfg.Overrides[item.key] = source
		}
	}

	err = cfg.Validate()
	if err != nil {
		return config.Config{}, fmt.Errorf("%w: %w", ErrInvalidSetting, err)
	}

	return cfg, nil
}

// applyFile loads the settings file and applies it to cfg.
//
// Parameters:
//   - cfg: the settings to update.
//   - flags: the parsed flags.
//   - lookupEnv: reads an environment variable.
//   - dirs: the user's XDG directories.
//
// Returns:
//   - error: an error wrapping ErrInvalidSetting.
func applyFile(cfg *config.Config, flags *pflag.FlagSet, lookupEnv LookupEnv, dirs config.Dirs) error {
	path, _, named := pick(flags, lookupEnv, setting{apply: nil, flag: flagConfig, env: config.EnvConfig, key: ""})
	if !named || path == "" {
		path, named = dirs.AppSettingsFile(), false
	}

	if path == "" {
		return nil
	}

	cfg.SettingsFile = path

	load := config.LoadOptionalFile
	if named {
		load = config.LoadFile
	}

	file, err := load(path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSetting, err)
	}

	err = file.Apply(cfg)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidSetting, path, err)
	}

	return nil
}

// pick returns the value that overrides a default, flags first.
//
// Parameters:
//   - flags: the parsed flags.
//   - lookupEnv: reads an environment variable.
//   - item: the setting.
//
// Returns:
//   - value: the overriding value.
//   - source: --flag or the environment variable name.
//   - ok: false when neither the flag nor the variable is set.
//
//nolint:nonamedreturns // Same-type returns need names.
func pick(flags *pflag.FlagSet, lookupEnv LookupEnv, item setting) (value, source string, ok bool) {
	if flags.Changed(item.flag) {
		return flags.Lookup(item.flag).Value.String(), "--" + item.flag, true
	}

	if lookupEnv == nil {
		return "", "", false
	}

	value, ok = lookupEnv(item.env)

	return value, item.env, ok
}

// applyMode sets the data source.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: hybrid or cache-only.
//
// Returns:
//   - error: the parse error.
func applyMode(cfg *config.Config, value string) error {
	mode, err := config.ParseMode(value)
	if err != nil {
		return fmt.Errorf("parse mode: %w", err)
	}

	cfg.Mode = mode

	return nil
}

// applyInterval sets the active polling interval.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: a duration such as 5m.
//
// Returns:
//   - error: the parse error.
func applyInterval(cfg *config.Config, value string) error {
	return parseDuration(value, &cfg.Interval)
}

// applyIdleInterval sets the idle polling interval.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: a duration such as 20m.
//
// Returns:
//   - error: the parse error.
func applyIdleInterval(cfg *config.Config, value string) error {
	return parseDuration(value, &cfg.IdleInterval)
}

// applyNotify sets the alert thresholds.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: thresholds such as 80,95, or empty.
//
// Returns:
//   - error: the parse error.
func applyNotify(cfg *config.Config, value string) error {
	thresholds, err := config.ParseThresholds(value)
	if err != nil {
		return fmt.Errorf("parse thresholds: %w", err)
	}

	cfg.Thresholds = thresholds

	return nil
}

// applyNotifyAuth toggles the login alert.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: a boolean.
//
// Returns:
//   - error: the parse error.
func applyNotifyAuth(cfg *config.Config, value string) error {
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("parse boolean: %w", err)
	}

	cfg.NotifyAuth = enabled

	return nil
}

// applyLogLevel sets the minimum log level.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: debug, info, warn, or error.
//
// Returns:
//   - error: the parse error.
func applyLogLevel(cfg *config.Config, value string) error {
	level, err := config.ParseLogLevel(value)
	if err != nil {
		return fmt.Errorf("parse log level: %w", err)
	}

	cfg.LogLevel = level

	return nil
}

// applyClaudeConfigDir sets Claude Code's config directory.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: a directory, or empty for the default.
//
// Returns:
//   - error: always nil.
func applyClaudeConfigDir(cfg *config.Config, value string) error {
	cfg.ClaudeDir = value

	return nil
}

// applyStateDir sets the state directory.
//
// An empty value keeps the XDG default.
//
// Parameters:
//   - cfg: the settings to update.
//   - value: a directory, or empty.
//
// Returns:
//   - error: always nil.
func applyStateDir(cfg *config.Config, value string) error {
	if value != "" {
		cfg.StateDir = value
	}

	return nil
}

// parseDuration parses a duration into target.
//
// Parameters:
//   - value: a duration such as 5m.
//   - target: where the duration is stored.
//
// Returns:
//   - error: the parse error.
func parseDuration(value string, target *time.Duration) error {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse duration: %w", err)
	}

	*target = parsed

	return nil
}
