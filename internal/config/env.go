// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

// Environment variables that override the settings file and the defaults.
const (
	// EnvConfig names the settings file.
	EnvConfig = "CLANKERWATCH_CONFIG"

	// EnvMode selects the data source.
	EnvMode = "CLANKERWATCH_MODE"

	// EnvInterval sets the polling interval while Claude Code is active.
	EnvInterval = "CLANKERWATCH_INTERVAL"

	// EnvIdleInterval sets the polling interval while Claude Code is idle.
	EnvIdleInterval = "CLANKERWATCH_IDLE_INTERVAL"

	// EnvNotify sets the alert thresholds. An empty value disables them.
	EnvNotify = "CLANKERWATCH_NOTIFY"

	// EnvNotifyAuth toggles the alert for a login that needs attention.
	EnvNotifyAuth = "CLANKERWATCH_NOTIFY_AUTH"

	// EnvLogLevel sets the minimum log level.
	EnvLogLevel = "CLANKERWATCH_LOG_LEVEL"

	// EnvStateDir overrides the state directory.
	EnvStateDir = "CLANKERWATCH_STATE_DIR"

	// EnvClaudeConfigDir is Claude Code's own config directory override.
	EnvClaudeConfigDir = "CLAUDE_CONFIG_DIR"
)
