---
title: "CLI Reference"
description: "Every clankerwatch command and flag, generated from the program itself"
weight: 7
type: docs
---

Complete command reference for clankerwatch, generated from the program itself.

clankerwatch publishes Claude Code subscription usage on the D-Bus session
bus for a KDE Plasma widget: the current 5-hour session, the weekly limits,
their reset times, and extra usage.

It reads Claude Code's login without ever refreshing or writing it, calls the
Anthropic usage endpoint at most every five minutes while Claude Code is
active, and adopts Claude Code's own cached result whenever that is newer.
Desktop notifications fire when usage crosses the alert thresholds.

Settings come from ~/.config/clankerwatch/config.yaml. An environment
variable overrides the file, and a flag overrides both.

## Commands

The systemd user unit runs `serve`, so you rarely run it yourself. `status` is the first thing to check when the widget shows something unexpected.

| Command | Description |
| --- | --- |
| [demo](/cli-reference/demo/) | Replay canned usage states for widget development |
| [notify-test](/cli-reference/notify-test/) | Send a sample desktop notification |
| [serve](/cli-reference/serve/) | Run the daemon |
| [status](/cli-reference/status/) | Print the settings and the current snapshot |
| [version](/cli-reference/version/) | Print version |

## Examples

### Run the daemon, as the systemd user unit does.

```bash
clankerwatch serve
```

### Print the settings and the snapshot the daemon would publish, offline.

```bash
clankerwatch status
```

### Replay canned states for widget development.

```bash
clankerwatch demo --notify ""
```

## Global options

Every command accepts these settings flags. A flag overrides its environment variable, which overrides the settings file. See [Configuration](/configuration/) for the details.

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
