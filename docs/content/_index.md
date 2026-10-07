---
title: clankerwatch
description: Claude Code subscription usage in the KDE Plasma panel, from a small daemon that reads Claude Code's login without ever changing it
type: docs
cascade:
  type: docs
---

**clankerwatch** puts your Claude Code subscription usage in the KDE Plasma panel: the current 5-hour session, the weekly limits, when each one resets, and extra usage. These are the same bars Claude Code's `/usage` shows, kept up to date without opening a terminal.

{{< video name="demo" poster="/images/popup.png" alt="The Clanker Watch popup cycling through normal, warning, critical, extra usage, rate limited, paused, and signed out states" >}}

It has two parts:

- **`clankerwatch`**, a small static Go daemon on the D-Bus session bus. D-Bus starts it on demand through a systemd user unit, so there is nothing to enable.
- **The Clanker Watch widget**, a Plasma 6 widget written in QML. It binds to the daemon's D-Bus properties, so updates are pushed to it. It never starts processes and never polls.

## How it gets the data

In the default **hybrid** mode the daemon reads Claude Code's login from `~/.claude/.credentials.json` without ever writing it, and asks `api.anthropic.com` for your usage at most every 5 minutes while Claude Code is in use and every 20 minutes while it is idle. Whenever Claude Code has fetched usage itself more recently, the daemon uses that cached result instead and saves a request.

In **cache-only** mode it never reads the login and never uses the network. It shows only what Claude Code has cached, so the bars can be stale.

The daemon never refreshes the login. Refresh tokens are single use, so a refresh by any other program would sign Claude Code out. When the token is about to expire, the daemon waits for Claude Code to refresh it the next time you use Claude Code. [Security](/security/) covers everything it reads and sends.

## What you need

| Requirement | Detail |
| --- | --- |
| KDE Plasma | 6.4 or later |
| systemd | A user session. D-Bus starts the daemon as a systemd user unit |
| Claude Code | Signed in with a Claude subscription, such as Pro or Max |
| Architecture | `amd64` or `arm64` Linux |

## Quick start

Download the package for your distribution from the [latest release](https://github.com/nicholas-fedor/clankerwatch/releases/latest) and install it, then right-click the panel, choose **Add Widgets**, and add **Clanker Watch**. [Installation](/installation/) has the commands for each distribution and for building from source.

## Documentation

- **[Getting started](/getting-started/)**: the first run, how the daemon starts, and what the panel and popup show.
- **[Installation](/installation/)**: packages, building from source, adding the widget, and removing everything.
- **[Configuration](/configuration/)**: the settings page, `config.yaml`, environment variables, and flags.
- **[How it works](/how-it-works/)**: data sources, scheduling and backoff, and the D-Bus contract.
- **[Security](/security/)**: how the daemon handles Claude Code's login and what it sends where.
- **[Troubleshooting](/troubleshooting/)**: what each status means and how to fix it.
- **[CLI reference](/cli-reference/)**: every command and flag.

## License

[AGPL-3.0-or-later](https://github.com/nicholas-fedor/clankerwatch/blob/main/LICENSE.md). clankerwatch is an independent project and is not affiliated with Anthropic.
