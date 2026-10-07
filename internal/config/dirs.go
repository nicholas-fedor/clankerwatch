// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// Dirs are the user directories the daemon builds paths from.
type Dirs struct {
	// Home is the user's home directory.
	Home string

	// StateHome is XDG_STATE_HOME, normally ~/.local/state.
	StateHome string

	// ConfigHome is XDG_CONFIG_HOME, normally ~/.config.
	ConfigHome string
}

// SystemDirs returns the current user's directories.
//
// The adrg/xdg package resolves them from the XDG environment variables with
// the specification's defaults.
//
// Returns:
//   - Dirs: the user's home, state, and config directories.
func SystemDirs() Dirs {
	return Dirs{
		Home:       xdg.Home,
		StateHome:  xdg.StateHome,
		ConfigHome: xdg.ConfigHome,
	}
}

// AppSettingsFile returns the default settings file.
//
// Returns:
//   - string: clankerwatch/config.yaml inside ConfigHome, or empty when
//     ConfigHome is unknown.
func (d Dirs) AppSettingsFile() string {
	if d.ConfigHome == "" {
		return ""
	}

	return filepath.Join(d.ConfigHome, appDirName, settingsFileName)
}

// AppStateDir returns the daemon's state directory.
//
// Returns:
//   - string: clankerwatch inside StateHome, or empty when StateHome is unknown.
func (d Dirs) AppStateDir() string {
	if d.StateHome == "" {
		return ""
	}

	return filepath.Join(d.StateHome, appDirName)
}
