---
title: "Demo"
description: "Serve canned usage on the D-Bus session bus, cycling through every state the widget shows: normal, warning, critical, extra usage, rate limited, paused, and..."
type: docs
---

Serve canned usage on the D-Bus session bus, cycling through every state the
widget shows: normal, warning, critical, extra usage, rate limited, paused,
and signed out, about every 12 seconds.

The real engine runs on scripted data, so alerts fire as they would for real.
Nothing reads Claude Code's files or uses the network. Stop the service first,
because only one process can own the bus name.

## Usage

```bash
clankerwatch demo
```

## Examples

### Replay states without sending desktop notifications.

```bash
systemctl --user stop clankerwatch
clankerwatch demo --notify "" --notify-auth=false
```

## Global options

These settings flags are accepted by every command. See [Configuration](/configuration/) for how they combine with environment variables and the settings file.

| Flag | Short | Default | Type | Description |
| --- | --- | --- | --- | --- |
| `--claude-config-dir` |  | `""` | string | Claude Code's config directory ($CLAUDE_CONFIG_DIR) |
| `--config` |  | `""` | string | YAML settings file, default $XDG_CONFIG_HOME/clankerwatch/config.yaml ($CLANKERWATCH_CONFIG) |
| `--idle-interval` |  | `20m0s` | duration | polling interval while Claude Code is idle ($CLANKERWATCH_IDLE_INTERVAL) |
| `--interval` |  | `5m0s` | duration | polling interval while Claude Code is active, at least 2m ($CLANKERWATCH_INTERVAL) |
| `--log-level` |  | `info` | string | debug, info, warn, or error ($CLANKERWATCH_LOG_LEVEL) |
| `--mode` |  | `hybrid` | string | data source: hybrid, or cache-only to never read credentials or use the network ($CLANKERWATCH_MODE) |
| `--notify` |  | `80,95` | string | alert thresholds in percent, empty to turn alerts off ($CLANKERWATCH_NOTIFY) |
| `--notify-auth` |  | `true` | bool | alert when the Claude Code login needs attention ($CLANKERWATCH_NOTIFY_AUTH) |
| `--state-dir` |  | `""` | string | directory for the daemon's state, default $XDG_STATE_HOME/clankerwatch ($CLANKERWATCH_STATE_DIR) |
