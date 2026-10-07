---
title: How it works
description: The daemon and widget architecture, where usage data comes from, when the daemon calls the usage endpoint, and the D-Bus contract the widget binds to
weight: 4
---

## Architecture

clankerwatch has two halves that talk over the D-Bus session bus:

```text
 Claude Code's files (read-only)          api.anthropic.com/api/oauth/usage
 ~/.claude/.credentials.json                       ^
 ~/.claude.json cachedUsageUtilization             | at most one request per 5 min while active
 ~/.claude/sessions/<pid>.json                     |
            \                                      |
             v                                     |
     clankerwatch serve   (static Go daemon, a D-Bus activated systemd user unit)
             |   state: ~/.local/state/clankerwatch/state.json
             |   settings: ~/.config/clankerwatch/config.yaml
             |   alerts: org.freedesktop.Notifications
             v
     session bus: com.nickfedor.ClankerWatch1 at /com/nickfedor/ClankerWatch1
             |   properties Snapshot, Settings, Version (JSON strings)
             |   methods Refresh(), SetSettings(s) -> s
             v
     Plasma widget com.nickfedor.clankerwatch   (pure QML)
```

- **The daemon owns all logic.** It decides when to fetch, keeps backoff and alert state across restarts, and publishes one JSON snapshot that changes only when the data does.
- **The widget only renders the snapshot.** It starts no processes and runs one 60-second timer to update countdown text such as "Resets in 2 hr".
- **The daemon starts on demand.** The widget's first property read activates it through D-Bus and systemd.

One goroutine in the daemon owns all state. Each time it wakes it reads Claude Code's files, decides whether to fetch, fetches when it should, sends any alerts, publishes the snapshot if its bytes changed, and saves the state if anything worth keeping changed.

## Data sources

The daemon reads three things from Claude Code's config directory, `~/.claude` by default or `CLAUDE_CONFIG_DIR` when set:

| File | What the daemon uses | When |
| --- | --- | --- |
| `.credentials.json` | The subscription login's access token, its expiry, its scopes, the plan, and when the whole login lapses | Hybrid mode only |
| `.claude.json` (in your home directory, or in `CLAUDE_CONFIG_DIR`) | The signed-in account and Claude Code's cached usage, `cachedUsageUtilization` | Always |
| `sessions/<pid>.json` | Whether a session changed recently or is busy, to choose the polling interval | Always |

Each reader checks the file's identity on every wake and reads it again only after it changed. Claude Code saves files by renaming a new file over the old one, so the check includes the inode, not just the modification time.

### Hybrid mode

Hybrid is the default. The daemon combines two sources and always shows the newest:

1. **The usage endpoint.** It calls `GET https://api.anthropic.com/api/oauth/usage` with Claude Code's access token, on the [schedule](#scheduling) below.
2. **Claude Code's cache.** Whenever Claude Code fetches usage itself, it saves the result in `.claude.json`. The daemon adopts that result when it is newer than its own data, which costs no request.

The snapshot's `source` field says which one the current data came from: `api` or `claude-code`.

### Cache-only mode

In cache-only mode the daemon never reads `.credentials.json` and never uses the network. Only Claude Code's cache feeds the bars, so they are as fresh as Claude Code's last fetch. See [Configuration](/configuration/#cache-only-mode).

### Adopting Claude Code's cache

The daemon takes Claude Code's cached usage only when all of these hold:

- It belongs to the account that is signed in now.
- It is newer than the data the daemon already has.
- It is not dated more than a minute in the future, which guards against clock skew.
- It parses as a usage payload.

When the signed-in account changes, the daemon drops the previous account's data at once instead of showing it under the new login.

## Scheduling

The daemon wakes at least every 2 minutes to look at the local files. Timers stop while the machine sleeps, so each wait is capped and every decision uses the wall clock. A laptop that resumes after a night picks up where it should, without a burst of requests.

### Gates

Before planning a fetch, the daemon checks what could block one. The first that applies wins, and it sets the status the widget shows:

| Gate | Status | The daemon waits for |
| --- | --- | --- |
| No credentials file, or no subscription login in it | `logged_out` | You to sign in with `/login` |
| The credentials file cannot be read or parsed | `auth_error` | The file to become readable |
| The token lacks the `user:profile` scope | `auth_error` | A new login with that scope |
| The endpoint rejected this token in the last hour | `auth_error` | A new token, or an hour to pass |
| The token expires within 5 minutes | `token_expired` | Claude Code to refresh the token the next time you use it |
| A backoff is running | `rate_limited`, `offline`, or `error` | The backoff to end |

Claude Code's cache keeps updating the bars while any gate holds. During a backoff, data that Claude Code fetched after the failures began also sets the status back to `ok`, while the backoff keeps running.

### When the next fetch is due

With no gate in the way:

- The next fetch is due one interval after the last data or the last attempt, whichever is later. The interval is `interval` (5 minutes) while Claude Code is in use and `idleInterval` (20 minutes) while it is idle.
- When a limit's window resets, a fetch is due 45 seconds after the reset, so the bar drops to zero promptly.
- After a fresh start with no saved data, the first fetch happens about 10 seconds after the daemon starts.
- A refresh from the widget makes a fetch due at once, but only when the last data and the last attempt are at least 2 minutes old.

### Backoff and Retry-After

A failed fetch starts a backoff whose length depends on the kind of failure. Each consecutive failure of the same kind doubles the wait, up to a cap:

| Failure | First wait | Cap | Status |
| --- | --- | --- | --- |
| HTTP 429, or a 403 that is not a scope error | 5 minutes | 1 hour | `rate_limited` |
| A transport error or timeout | 1 minute | 10 minutes | `offline` |
| Another HTTP error, or a payload the parser does not recognize | 2 minutes | 30 minutes | `error` |

The daemon retries when the backoff ends, even when that comes before the polling interval, and keeps the error status until then. If Claude Code saves newer usage in the meantime, the daemon adopts it and skips the retry.

For rate limits, the server's `Retry-After` header is honored as a floor. It may be given in seconds or as an HTTP date, and the result is still capped at one hour. A 403 counts as a rate limit, as it does for Claude Code, unless the error says the token lacks a permission.

A 401, or a 403 for a missing scope, does not start a backoff. The daemon blocks that specific token for an hour instead. It recognizes the token by its expiry time, so it never stores the token itself, and a new login clears the block immediately.

The backoff and the token block are saved in the state file, so restarting the daemon cannot skip a `Retry-After`.

## The D-Bus contract

The daemon owns `com.nickfedor.ClankerWatch1` on the session bus and exports one object at `/com/nickfedor/ClankerWatch1` with the interface `com.nickfedor.ClankerWatch1`.

| Member | Kind | Contents |
| --- | --- | --- |
| `Snapshot` | property `s` | Usage, status, and timing as JSON. Emits `PropertiesChanged` only when it changes |
| `Settings` | property `s` | Editable settings, the locked keys, and the settings file path as JSON |
| `Version` | property `s` | The daemon version. Constant |
| `Refresh()` | method | Asks for a fetch. The daemon decides whether to honor it |
| `SetSettings(s) -> s` | method | Applies a JSON change. Returns an error message, or an empty string on success |

Properties are JSON strings, so every update is atomic and QML never decodes D-Bus container types. The object is fully exported before the daemon claims the bus name, so a widget that activated the daemon never sees a half-built object. Both JSON documents carry `"v": 1`, which changes with any incompatible schema change.

Read them with `busctl`:

```bash
busctl --user get-property com.nickfedor.ClankerWatch1 \
  /com/nickfedor/ClankerWatch1 com.nickfedor.ClankerWatch1 Snapshot
busctl --user call com.nickfedor.ClankerWatch1 \
  /com/nickfedor/ClankerWatch1 com.nickfedor.ClankerWatch1 Refresh
```

### Snapshot

Times are milliseconds since the Unix epoch, and `0` means none. No field changes on every wake, so an unchanged snapshot is never sent again.

| Field | Type | Meaning |
| --- | --- | --- |
| `v` | number | Schema version, `1` |
| `status` | string | `starting`, `ok`, `rate_limited`, `offline`, `error`, `token_expired`, `logged_out`, `auth_error`, or `no_data` |
| `message` | string | A sentence explaining the status, empty for `ok` and `starting` |
| `mode` | string | `hybrid` or `cache-only` |
| `source` | string | Where the data came from: `api`, `claude-code`, or empty without data |
| `plan` | string | The plan, such as `Pro`, or empty when unknown |
| `active` | boolean | Whether Claude Code counts as in use |
| `fetchedAtMs` | number | When the current data was fetched |
| `nextUpdateAtMs` | number | When the next fetch is planned, `0` while waiting on something outside the daemon |
| `refreshAllowedAtMs` | number or null | When a refresh is honored. `0` means now, and `null` means a refresh cannot help |
| `loginExpiresAtMs` | number | When the whole Claude Code login lapses and needs `/login` |
| `warnAt` | number | The warning color threshold in percent |
| `critAt` | number | The critical color threshold in percent |
| `bars` | array | The limits, in the order Claude Code's `/usage` shows them |
| `extraUsage` | object or null | Extra usage, when the plan has it |

Each entry in `bars`:

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | A stable identifier, such as `session` |
| `kind` | string | The server's kind: `session`, `weekly_all`, `weekly_scoped`, or `credit` |
| `group` | string | The server's grouping, such as `session` or `weekly` |
| `label` | string | The long label, such as "Current week (all models)" |
| `short` | string | The panel label, such as `7d` |
| `percent` | number | The percentage used, which can exceed 100 |
| `resetsAtMs` | number | When the window resets |
| `severity` | string | The server's own grading, such as `warning` or `critical` |
| `level` | number | The level the widget colors by: `0` normal, `1` warning, `2` critical |
| `headline` | boolean | Marks the bar a single-value indicator shows |
| `credit` | boolean | Marks a credit balance, which is never alerted on |

`level` is the more urgent of the server's `severity` and the configured thresholds.

`extraUsage` has `percent` (number or null), `used` and `limit` (formatted amounts, empty when unknown), `level`, and `limitReached`.

### Settings

| Field | Type | Meaning |
| --- | --- | --- |
| `v` | number | Schema version, `1` |
| `mode` | string | `hybrid` or `cache-only` |
| `intervalSeconds` | number | The active polling interval |
| `idleIntervalSeconds` | number | The idle polling interval |
| `minIntervalSeconds` | number | The shortest allowed interval, `120` |
| `notify` | array | The alert thresholds in percent, empty when alerts are off |
| `notifyAuth` | boolean | Whether the login alert is on |
| `locked` | object | Each key an environment variable or flag holds, mapped to that source, such as `{"interval": "CLANKERWATCH_INTERVAL"}` |
| `file` | string | The settings file path |
| `editable` | boolean | `false` when the daemon cannot change settings, such as in demo mode |

`SetSettings` takes a JSON object with any of `mode`, `intervalSeconds`, `idleIntervalSeconds`, `notify`, and `notifyAuth`. Absent fields stay unchanged, and unknown fields are rejected:

```bash
busctl --user call com.nickfedor.ClankerWatch1 \
  /com/nickfedor/ClankerWatch1 com.nickfedor.ClankerWatch1 \
  SetSettings s '{"intervalSeconds": 600}'
```

The daemon writes the change into `config.yaml`, keeping its comments, and resolves the settings again exactly as it does at startup. If the result does not validate, it restores the previous file and returns the error. A change to a locked key is refused.

## Efficiency

clankerwatch is meant to be invisible in `top`:

- The widget never polls. The daemon pushes changes, and an unchanged snapshot emits no signal.
- The daemon sleeps between wakes, at most every 2 minutes, and reads a file only after it changed.
- Requests to the usage endpoint are minutes apart, so HTTP keep-alive is off and no connection stays open.
- Responses are capped at 1 MiB and each request times out after 15 seconds.
- The unit runs in `background.slice` with `GOMAXPROCS=2`, a Go memory limit of 24 MiB, `MemoryHigh=32M`, `MemoryMax=64M`, and `TasksMax=32`.
- The popup stays light because Plasma preloads it: no continuous animations, and colors and fonts come from the Plasma theme.

## State

The daemon keeps `state.json` in `$XDG_STATE_HOME/clankerwatch`, normally `~/.local/state/clankerwatch`. It holds:

- The last good usage payload, with when and where it came from and its account.
- The backoff and any token block, so a restart cannot ignore a `Retry-After`.
- The alerts already delivered, so an alert is never repeated, even across restarts.

It never contains the token. The daemon replaces the file through a temporary file and a rename, so a crash never leaves it half written. The file is created with mode `0600` in a directory with mode `0700`.
