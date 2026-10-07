---
title: Security
description: How clankerwatch handles Claude Code's login, what it reads and sends where, why it has no login of its own, and how the service is confined
weight: 6
---

clankerwatch reads Claude Code's login so it can ask for your usage. That makes it responsible for a credential it did not issue, and the rules below exist to keep that use minimal and safe. A policy check in the build fails if token refresh code ever appears in the program.

## Read-only credential handling

- **It never writes Claude Code's files.** Every reader opens Claude Code's files for reading only. The daemon's own files live in its own directories.
- **It reads only what it needs.** From `~/.claude/.credentials.json` it decodes the subscription login's access token, expiry, scopes, plan, and login lifetime. The decoder skips everything else, including the refresh token and any MCP server logins.
- **It never opens the `.key` files.** Claude Code keeps secrets in `.key` files next to its session files in `~/.claude/sessions`. The daemon reads only files named `<pid>.json` there, to tell whether Claude Code is in use.
- **The token stays in memory.** It is held in a type that prints as a redaction marker in every format, in logs, and in JSON, so it cannot leak through a log line or a debug dump. File buffers that held it are cleared after parsing.
- **The token is never stored.** The state file holds the usage data, the backoff, and the alerts already sent. A rejected token is remembered by its expiry time, never by its value.

## It never refreshes the token

Claude Code's OAuth refresh tokens rotate and are single use. If any other program used one, Claude Code's copy would stop working and you would be signed out.

So when the access token is about to expire, the daemon stops calling the endpoint and shows "Paused until Claude Code is used again". The next time you use Claude Code, it refreshes the token itself, and the daemon picks up the new one from the credentials file within 2 minutes.

## Where the token goes

The token is sent in exactly one request: `GET https://api.anthropic.com/api/oauth/usage`, with the token as a bearer credential.

- The base URL is fixed in the program and HTTPS only.
- The client refuses to follow redirects, so the token cannot be forwarded to another host.
- Each request times out after 15 seconds, and a response over 1 MiB is rejected.
- The client honors the standard `HTTPS_PROXY` and `NO_PROXY` variables. Through a proxy, TLS still runs end to end with api.anthropic.com.

The daemon makes no other network connection. It sends no telemetry, checks for no updates, and never contacts claude.ai. The popup's **Open the claude.ai usage page** button opens your browser and does nothing else.

## An honest User-Agent

Every request identifies itself as `clankerwatch/<version>`. The daemon never poses as Claude Code, so Anthropic can see exactly which program made the request and how often.

## No login of its own

clankerwatch has no sign-in flow and never will. Anthropic does not allow third-party applications to offer Claude.ai login, so the only credential available is the one Claude Code already holds. Reading it without changing it, at a low and honest request rate, is the least intrusive way to show your usage.

Anthropic's terms do not cover personal, read-only use of Claude Code's login by another program. Hybrid mode keeps that use minimal. By default it makes one request every 5 minutes while Claude Code is in use and every 20 minutes while it is idle. A refresh you ask for is honored at most every 2 minutes. When Claude Code's own cached result is newer, the daemon uses it instead of asking, and while the endpoint asks the daemon to back off, it makes no request at all.

## Cache-only mode

If you prefer that no program other than Claude Code use its login, set `mode: cache-only`. The daemon then never opens `.credentials.json` and never uses the network. It shows the usage Claude Code cached in `~/.claude.json`, which is as fresh as Claude Code's own last fetch. See [Configuration](/configuration/#cache-only-mode).

## The daemon's own files

| File | Contents | Mode |
| --- | --- | --- |
| `~/.config/clankerwatch/config.yaml` | Settings, no secrets | `0600`, directory `0700` when the daemon creates it |
| `~/.local/state/clankerwatch/state.json` | Usage data, backoff, delivered alerts | `0600`, directory `0700` |

Both files are replaced atomically through a temporary file and a rename.

## systemd hardening

The user unit confines the daemon. `systemd-analyze --user security clankerwatch` rates it 1.5 (OK).

| Setting | Effect |
| --- | --- |
| `ProtectSystem=strict`, `ProtectHome=read-only` | The whole file system is read-only, including your home directory |
| `ReadWritePaths=%E/clankerwatch %S/clankerwatch` | The only writable places: the settings directory and the state directory |
| `PrivateTmp=yes`, `PrivateDevices=yes`, `PrivateMounts=yes` | Its own empty `/tmp`, no hardware devices, and no mount changes seen by others |
| `ProtectKernelTunables=yes`, `ProtectKernelModules=yes`, `ProtectKernelLogs=yes`, `ProtectControlGroups=yes`, `ProtectClock=yes`, `ProtectHostname=yes` | No kernel settings, modules, logs, cgroups, clock, or hostname changes |
| `CapabilityBoundingSet=`, `AmbientCapabilities=` | No capabilities at all |
| `KeyringMode=private` | No shared kernel keyring |
| `NoNewPrivileges=yes` | No privilege gain through setuid binaries or file capabilities |
| `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` | Only the session bus and HTTPS sockets |
| `SystemCallFilter=@system-service ~@privileged ~@resources` | Only the system calls a typical service needs, without privileged or resource-control calls. Others fail with `EPERM` |
| `SystemCallArchitectures=native` | No system calls through a foreign ABI |
| `MemoryDenyWriteExecute=yes` | No memory that is both writable and executable |
| `LockPersonality=yes` | The execution domain cannot change |
| `RestrictRealtime=yes` | No realtime scheduling |
| `RestrictSUIDSGID=yes` | Cannot create setuid or setgid files |
| `RestrictNamespaces=yes` | Cannot create namespaces |
| `UMask=0077` | Files it creates are private to you |
| `MemoryMax=64M`, `TasksMax=32` | A runaway daemon cannot exhaust memory or processes |

The daemon runs as your user, because it needs to read Claude Code's files in your home directory. `/proc` stays visible, because the daemon checks whether Claude Code's processes are alive.

A `stateDir` outside `~/.local/state` is not writable in this sandbox. Add it with a drop-in:

```sh
systemctl --user edit clankerwatch
```

```ini
[Service]
ReadWritePaths=/path/to/state
```

## Releases

Release binaries are static, built with `CGO_ENABLED=0` and `-trimpath`. Each release publishes SBOMs and a `checksums.txt` signed with keyless Sigstore cosign. [Installation](/installation/#verify-a-download) shows how to verify a download.

## Reporting a vulnerability

Report it privately through [GitHub Security Advisories](https://github.com/nicholas-fedor/clankerwatch/security/advisories/new). Please do not open a public issue for a vulnerability.
