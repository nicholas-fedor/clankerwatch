---
title: Troubleshooting
description: Find out why the widget shows an error or stale data, what each status means, and how to fix it
weight: 5
---

## Start here

Two commands answer most questions:

```bash
clankerwatch status
journalctl --user -u clankerwatch -n 50
```

`clankerwatch status` prints the settings the daemon resolves, the Claude Code files it reads, the settings file, the state file, and the snapshot it would publish right now. It reads local files only and never calls the usage endpoint, so it is safe to run as often as you like. Add `--json` for machine-readable output:

```bash
clankerwatch status --json | jq .snapshot
```

The snapshot's `status` and `message` fields match what the widget shows. The sections below go through each status. `clankerwatch status` resolves settings from your shell's environment, so a variable you set only in the service's drop-in does not apply to it.

For more detail in the journal, turn on debug logging and reload:

```bash
systemctl --user edit clankerwatch     # add: Environment=CLANKERWATCH_LOG_LEVEL=debug
systemctl --user restart clankerwatch
journalctl --user -u clankerwatch -f
```

Or set `logLevel: debug` in `config.yaml` and run `systemctl --user reload clankerwatch`. The token never appears in the log at any level.

## The service is not running

The widget says "clankerwatch isn't running" and the footer says "Service unavailable".

Check the unit:

```bash
systemctl --user status clankerwatch
```

| What you see | Cause | Fix |
| --- | --- | --- |
| `Unit clankerwatch.service could not be found` | systemd has not loaded the unit | Run `systemctl --user daemon-reload`. For a source install, run `task install` again |
| `status=2` in the journal with `invalid setting` | A setting is invalid, such as `interval: 1m` or an unknown key | Fix the file or variable the error names, then `systemctl --user restart clankerwatch` |
| `another clankerwatch instance owns the bus name` | Another daemon, often `clankerwatch demo`, holds the bus name | Stop the other process, then `systemctl --user start clankerwatch` |
| `connect to the session bus` | No session bus in the daemon's environment | Make sure you are in a graphical Plasma session with a systemd user session |
| The unit is inactive with no errors | Nothing has activated it yet | Press **Try again** in the popup, or run `systemctl --user start clankerwatch` |

If the unit starts by hand but the widget never activates it, the session bus may not know the activation file yet. For a source install, ask it to re-read its configuration:

```bash
busctl --user call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus ReloadConfig
```

Logging out and back in has the same effect.

## logged_out

The widget says "Claude Code isn't signed in", and the footer says "No credentials found".

The daemon found no `.credentials.json`, or the file has no subscription login, for example after `/logout` or when Claude Code uses an API key instead of a subscription.

1. Run `claude` in a terminal and sign in with `/login`, using your Claude subscription.
2. The bars appear within 2 minutes, with no restart needed.

If Claude Code is signed in but the daemon still reports `logged_out`, it is probably looking in the wrong directory. `clankerwatch status` shows the credentials path it uses. If you run Claude Code with `CLAUDE_CONFIG_DIR`, give the daemon the same directory with the `claudeConfigDir` key. See [Configuration](/configuration/#environment-variables-and-flags).

After 10 minutes in this state you get one notification, unless `notifyAuth` is off.

## token_expired

The footer says "Paused until Claude Code is used again".

This is normal and needs no action. Claude Code's access token is about to expire, and only Claude Code may refresh it, because refresh tokens are single use. The daemon stops calling the endpoint until the token is fresh again. The next time you use Claude Code, it refreshes the token and the daemon resumes within 2 minutes. Until then the bars keep the last data, and they still update whenever Claude Code caches new usage. See [Security](/security/#it-never-refreshes-the-token).

This state never triggers a notification.

## rate_limited

The footer says "Rate limited · data from …" and "Retry in …".

The usage endpoint is heavily rate limited, and the limit is shared with Claude Code. The daemon backs off for 5 minutes, doubling on each consecutive rate limit up to an hour, and never retries sooner than the server's `Retry-After`. The backoff survives restarts, so restarting the daemon does not help.

Meanwhile the bars keep the last data, and any newer usage that Claude Code fetches replaces it. If you see this often:

- Leave `interval` at its default of 5 minutes or raise it.
- Avoid requesting refreshes repeatedly. The daemon already ignores refreshes closer than 2 minutes apart.
- Consider [cache-only mode](/configuration/#cache-only-mode), which never calls the endpoint.

A 403 that is not a scope error also counts as a rate limit, as it does for Claude Code.

## auth_error

The widget says "Claude Code's login was not accepted", followed by one of these messages:

| Message | Cause | Fix |
| --- | --- | --- |
| Claude Code's credentials cannot be read | `.credentials.json` exists but cannot be read or parsed | Check the file's permissions. Signing in again with `/login` rewrites it |
| Claude Code's login lacks the user:profile scope | The login was made without the scope the usage endpoint needs | Run `claude` and sign in again with `/login` |
| The usage endpoint rejected Claude Code's login | The endpoint answered 401, or 403 for a missing scope | Run `claude` and sign in again with `/login` |

After a rejection the daemon blocks that token for an hour. A new login clears the block at once, because it comes with a new token. After 10 minutes in this state you get one notification, unless `notifyAuth` is off.

## offline and error

The footer says "Offline · data from …" or "Usage endpoint error · data from …", with "Retry in …".

- **offline**: api.anthropic.com cannot be reached, or the request timed out after 15 seconds. The daemon retries after 1 minute, doubling up to 10 minutes. Check your network and any proxy settings.
- **error**: the endpoint returned an HTTP error, or a payload the daemon does not recognize. The daemon retries after 2 minutes, doubling up to 30 minutes. The journal has the HTTP status and the API's error message.

The usage payload is undocumented and changes over time. If `error` persists with "unrecognized payload" in the journal while Claude Code's `/usage` works, please [open an issue](https://github.com/nicholas-fedor/clankerwatch/issues) with the clankerwatch version.

## no_data

The widget says "No usage data yet" and the footer says "Waiting for Claude Code".

This only happens in cache-only mode. Claude Code has not cached any usage for the signed-in account yet. Open `/usage` in Claude Code once, and the daemon picks up the result within 2 minutes.

## The bars look stale

The footer always says how old the data is. If it is older than you expect:

- In cache-only mode, data is only as fresh as Claude Code's last fetch.
- While Claude Code is idle, the daemon checks every 20 minutes by default.
- During `token_expired` or a backoff, the daemon does not call the endpoint.

`clankerwatch status` shows the `nextUpdateAtMs` and `source` of the current data.

## The widget does not change after an update

Plasma loads a widget's QML only when plasmashell starts, so a new widget version needs a restart:

```bash
systemctl --user restart plasma-plasmashell
```

The panel blinks briefly while it restarts. Widget errors appear in plasmashell's journal:

```bash
journalctl --user -u plasma-plasmashell -f
```

Also restart the daemon after updating, so it runs the new binary:

```bash
systemctl --user restart clankerwatch
```

## No notifications

1. Run `clankerwatch notify-test`. If no notification appears, check Plasma's notification settings and Do Not Disturb.
2. Check that `notify` is not empty and `notifyAuth` is on, in the settings page or with `clankerwatch status`.
3. Each threshold alerts once per reset window. A bar that was already above a threshold when the window started alerts only once, and a restart does not repeat it.

## The settings page is disabled

| Message | Meaning |
| --- | --- |
| clankerwatch isn't running. The settings appear once the service starts. | The page reads the settings from the daemon. Start it as described in [The service is not running](#the-service-is-not-running) |
| The service is running in demo mode, so settings can't be changed. | Stop `clankerwatch demo` and start the real daemon |
| Set by an environment variable or command-line flag: … | Those keys are [locked](/configuration/#locked-keys). Change the variable or flag instead |
