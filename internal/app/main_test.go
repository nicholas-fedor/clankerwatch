// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// TestMain runs the package's tests against an empty temporary home.
//
// Most tests execute the command tree, which resolves the real XDG
// directories. Pointing HOME and every XDG base directory at a temporary
// directory, and clearing the daemon's variables, keeps the user's settings
// file, state, and Claude Code's files out of every test.
//
// Parameters:
//   - m: the test runner.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

// runIsolated runs the tests in a temporary home and removes it afterwards.
//
// Parameters:
//   - m: the test runner.
//
// Returns:
//   - int: the exit code of the test run.
func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "clankerwatch-app")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "create a temporary home:", err)

		return 1
	}

	defer func() { _ = os.RemoveAll(home) }()

	err = isolate(home)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "isolate the environment:", err)

		return 1
	}

	return m.Run()
}

// isolate points the environment at home and reloads adrg/xdg.
//
// Parameters:
//   - home: the temporary home directory.
//
// Returns:
//   - error: the first environment change that failed.
func isolate(home string) error {
	set := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
	}

	for key, value := range set {
		err := os.Setenv(key, value)
		if err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}

	for _, key := range []string{
		config.EnvConfig, config.EnvMode, config.EnvInterval, config.EnvIdleInterval, config.EnvNotify,
		config.EnvNotifyAuth, config.EnvLogLevel, config.EnvStateDir, config.EnvClaudeConfigDir,
	} {
		err := os.Unsetenv(key)
		if err != nil {
			return fmt.Errorf("unset %s: %w", key, err)
		}
	}

	xdg.Reload()

	return nil
}
