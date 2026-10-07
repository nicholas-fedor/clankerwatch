// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// Report is what the status command prints.
type Report struct {
	// Version is the daemon version.
	Version string `json:"version"`

	// Mode is the data source.
	Mode config.Mode `json:"mode"`

	// Credentials is Claude Code's credentials file.
	Credentials string `json:"credentials"`

	// GlobalConfig is Claude Code's global config file.
	GlobalConfig string `json:"globalConfig"`

	// Sessions is Claude Code's session status directory.
	Sessions string `json:"sessions"`

	// StateFile is the daemon's state file.
	StateFile string `json:"stateFile"`

	// SettingsFile is the YAML settings file path. The file may not exist.
	SettingsFile string `json:"settingsFile"`

	// Snapshot is the snapshot JSON the daemon would publish.
	Snapshot json.RawMessage `json:"snapshot"`

	// Thresholds are the alert thresholds.
	Thresholds []float64 `json:"thresholds"`

	// Interval is the polling interval while Claude Code is active.
	Interval time.Duration `json:"interval"`

	// IdleInterval is the polling interval while Claude Code is idle.
	IdleInterval time.Duration `json:"idleInterval"`

	// NotifyAuth reports whether login alerts are on.
	NotifyAuth bool `json:"notifyAuth"`
}

// newReport assembles a status report.
//
// Parameters:
//   - version: the daemon version.
//   - cfg: the settings.
//   - paths: Claude Code's file locations.
//   - snapshot: the snapshot JSON.
//
// Returns:
//   - Report: the report.
func newReport(version string, cfg config.Config, paths claudecode.Paths, snapshot string) Report {
	return Report{
		Version:      version,
		Mode:         cfg.Mode,
		Credentials:  paths.Credentials,
		GlobalConfig: paths.GlobalConfig,
		Sessions:     paths.Sessions,
		StateFile:    cfg.StateFile(),
		SettingsFile: cfg.SettingsFile,
		Snapshot:     json.RawMessage(snapshot),
		Thresholds:   cfg.Thresholds,
		Interval:     cfg.Interval,
		IdleInterval: cfg.IdleInterval,
		NotifyAuth:   cfg.NotifyAuth,
	}
}
