---
title: "Status"
description: "Print the resolved settings, the files clankerwatch reads, and the snapshot the daemon would publish right now."
type: docs
---

Print the resolved settings, the files clankerwatch reads, and the snapshot
the daemon would publish right now.

It reads Claude Code's files and the saved state only. It never calls the
usage endpoint, so it is safe to run at any time.

## Usage

```bash
clankerwatch status
```

## Examples

### Show the settings and snapshot.

```bash
clankerwatch status
```

### Print the report as JSON.

```bash
clankerwatch status --json | jq .snapshot.bars
```

## Options

| Flag | Short | Default | Type | Description |
| --- | --- | --- | --- | --- |
| `--json` |  | `false` | bool | print the report as JSON |

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
