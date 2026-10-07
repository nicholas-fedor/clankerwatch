// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
)

// Deps are the engine's collaborators.
//
// Credentials and Fetcher are unused in cache-only mode, and a nil Notifier
// disables alerts.
type Deps struct {
	// Credentials reads Claude Code's login.
	Credentials CredentialsSource

	// Global reads Claude Code's global config.
	Global GlobalSource

	// Activity reports whether Claude Code is in use.
	Activity ActivitySource

	// Fetcher retrieves usage payloads.
	Fetcher Fetcher

	// Notifier sends alerts, or is nil.
	Notifier Notifier

	// Store persists state across restarts.
	Store Store

	// Logger receives diagnostics, or is nil to discard them.
	Logger Logger
}

// Options are the engine's settings.
type Options struct {
	// Mode selects the data source.
	Mode config.Mode

	// Thresholds are the alert thresholds, ascending.
	Thresholds []float64

	// Policy is the schedule.
	Policy schedule.Policy

	// Warn is the warning color threshold.
	Warn float64

	// Crit is the critical color threshold.
	Crit float64

	// NotifyAuth enables the login alert.
	NotifyAuth bool
}

// OptionsFrom derives engine options from the daemon config.
//
// Parameters:
//   - cfg: the daemon config.
//
// Returns:
//   - Options: options with the production schedule.
func OptionsFrom(cfg config.Config) Options {
	warn, crit := cfg.ColorThresholds()

	return Options{
		Mode:       cfg.Mode,
		Policy:     schedule.DefaultPolicy(cfg.Interval, cfg.IdleInterval),
		Thresholds: cfg.Thresholds,
		Warn:       warn,
		Crit:       crit,
		NotifyAuth: cfg.NotifyAuth,
	}
}
