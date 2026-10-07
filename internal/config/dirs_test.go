// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
)

// TestAppStateDir nests the app directory in StateHome.
func TestAppStateDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	assert.Equal(t, filepath.Join(root, "clankerwatch"), Dirs{StateHome: root}.AppStateDir())
	assert.Empty(t, Dirs{Home: root}.AppStateDir(), "an unknown StateHome gives no directory")
}

// TestAppSettingsFile places config.yaml in the app directory of ConfigHome.
func TestAppSettingsFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	assert.Equal(t, filepath.Join(root, "clankerwatch", "config.yaml"), Dirs{ConfigHome: root}.AppSettingsFile())
	assert.Empty(t, Dirs{Home: root}.AppSettingsFile(), "an unknown ConfigHome gives no file")
}

// TestSystemDirsFollowsXDG reads the directories from the XDG environment.
//
// The adrg/xdg package caches the environment at init, so the test reloads it
// after pointing HOME, XDG_STATE_HOME, and XDG_CONFIG_HOME at a temporary
// directory, and again
// after the variables are restored. It cannot run in parallel because it
// changes the environment and the package's globals.
func TestSystemDirsFollowsXDG(t *testing.T) {
	// Registered before t.Setenv, so it runs after the variables are restored.
	t.Cleanup(xdg.Reload)

	root := t.TempDir()
	home := filepath.Join(root, "home")
	state := filepath.Join(root, "state")
	configHome := filepath.Join(root, "config")

	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	xdg.Reload()

	got := SystemDirs()
	assert.Equal(t, home, got.Home)
	assert.Equal(t, state, got.StateHome)
	assert.Equal(t, filepath.Join(state, "clankerwatch"), got.AppStateDir())
	assert.Equal(t, configHome, got.ConfigHome)
	assert.Equal(t, filepath.Join(configHome, "clankerwatch", "config.yaml"), got.AppSettingsFile())
}

// TestSystemDirsDefaultStateHome falls back to ~/.local/state.
//
// The XDG specification defines that default when XDG_STATE_HOME is unset or
// empty.
func TestSystemDirsDefaultStateHome(t *testing.T) {
	t.Cleanup(xdg.Reload)

	home := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	xdg.Reload()

	got := SystemDirs()
	assert.Equal(t, home, got.Home)
	assert.Equal(t, filepath.Join(home, ".local", "state"), got.StateHome)
}
