// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"os"
	"path/filepath"
)

// Paths locates Claude Code's files.
type Paths struct {
	// Dir is Claude Code's config directory.
	Dir string

	// Credentials is the OAuth credentials file.
	Credentials string

	// GlobalConfig is the global state file, which includes the cached usage.
	GlobalConfig string

	// Sessions holds one small status file per running session.
	Sessions string
}

const (
	// defaultDirName is Claude Code's config directory inside the home directory.
	defaultDirName = ".claude"

	// credentialsName is the credentials file inside the config directory.
	credentialsName = ".credentials.json"

	// globalConfigName is the global state file.
	globalConfigName = ".claude.json"

	// legacyGlobalConfigName is the older global state file. When it exists it
	// takes precedence, as it does for Claude Code.
	legacyGlobalConfigName = ".config.json"

	// sessionsName is the session status directory inside the config directory.
	sessionsName = "sessions"
)

// ResolvePaths mirrors how Claude Code picks its locations.
//
// Parameters:
//   - claudeConfigDir: the value of CLAUDE_CONFIG_DIR, or empty.
//   - home: the user's home directory.
//
// Returns:
//   - Paths: the resolved file locations.
func ResolvePaths(claudeConfigDir, home string) Paths {
	dir := claudeConfigDir
	if dir == "" {
		dir = filepath.Join(home, defaultDirName)
	}

	globalBase := claudeConfigDir
	if globalBase == "" {
		globalBase = home
	}

	global := filepath.Join(globalBase, globalConfigName)

	legacy := filepath.Join(dir, legacyGlobalConfigName)
	if isRegularFile(legacy) {
		global = legacy
	}

	return Paths{
		Dir:          dir,
		Credentials:  filepath.Join(dir, credentialsName),
		GlobalConfig: global,
		Sessions:     filepath.Join(dir, sessionsName),
	}
}

// isRegularFile reports whether path names an existing regular file.
//
// Parameters:
//   - path: the file to check.
//
// Returns:
//   - bool: true for a regular file.
func isRegularFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}
