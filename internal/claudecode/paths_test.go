// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolvePaths mirrors Claude Code's choice of locations.
func TestResolvePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup     func(t *testing.T, home, configDir string)
		wantDir   func(home, configDir string) string
		wantGlob  func(home, configDir string) string
		name      string
		useConfig bool
	}{
		{
			name:     "home directory",
			wantDir:  func(home, _ string) string { return filepath.Join(home, ".claude") },
			wantGlob: func(home, _ string) string { return filepath.Join(home, ".claude.json") },
		},
		{
			name:      "CLAUDE_CONFIG_DIR",
			useConfig: true,
			wantDir:   func(_, configDir string) string { return configDir },
			wantGlob:  func(_, configDir string) string { return filepath.Join(configDir, ".claude.json") },
		},
		{
			name: "legacy config in the home layout",
			setup: func(t *testing.T, home, _ string) {
				t.Helper()

				dir := filepath.Join(home, ".claude")
				require.NoError(t, os.MkdirAll(dir, 0o700))
				writeFile(t, filepath.Join(dir, ".config.json"), "{}", fixedModTime)
			},
			wantDir:  func(home, _ string) string { return filepath.Join(home, ".claude") },
			wantGlob: func(home, _ string) string { return filepath.Join(home, ".claude", ".config.json") },
		},
		{
			name:      "legacy config in CLAUDE_CONFIG_DIR",
			useConfig: true,
			setup: func(t *testing.T, _, configDir string) {
				t.Helper()

				writeFile(t, filepath.Join(configDir, ".config.json"), "{}", fixedModTime)
			},
			wantDir:  func(_, configDir string) string { return configDir },
			wantGlob: func(_, configDir string) string { return filepath.Join(configDir, ".config.json") },
		},
		{
			name: "legacy path that is a directory",
			setup: func(t *testing.T, home, _ string) {
				t.Helper()

				require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", ".config.json"), 0o700))
			},
			wantDir:  func(home, _ string) string { return filepath.Join(home, ".claude") },
			wantGlob: func(home, _ string) string { return filepath.Join(home, ".claude.json") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			configDir := t.TempDir()

			if tt.setup != nil {
				tt.setup(t, home, configDir)
			}

			env := ""
			if tt.useConfig {
				env = configDir
			}

			got := ResolvePaths(env, home)
			dir := tt.wantDir(home, configDir)

			assert.Equal(t, Paths{
				Dir:          dir,
				Credentials:  filepath.Join(dir, ".credentials.json"),
				GlobalConfig: tt.wantGlob(home, configDir),
				Sessions:     filepath.Join(dir, "sessions"),
			}, got)
		})
	}
}

// TestIsRegularFile accepts only existing regular files.
func TestIsRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	writeFile(t, file, "x", fixedModTime)

	assert.True(t, isRegularFile(file))
	assert.False(t, isRegularFile(dir))
	assert.False(t, isRegularFile(filepath.Join(dir, "missing")))
}
