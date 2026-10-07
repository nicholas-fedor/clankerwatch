---
title: Installation
description: Install clankerwatch from a release package or from source, add the widget to a panel, and remove it again
weight: 2
---

## Requirements

| Requirement | Detail |
| --- | --- |
| KDE Plasma | 6.4 or later. The widget declares Plasma API 6.4 as its minimum. |
| systemd user session | The daemon runs as a systemd user unit that D-Bus starts on demand. |
| Claude Code | Signed in with a Claude subscription. The daemon reads its login and its cached usage. |
| Architecture | Linux on `amd64` or `arm64`. |

The daemon is a static binary with no runtime dependencies. The widget uses only modules that ship with Plasma.

## Release packages

Every [release](https://github.com/nicholas-fedor/clankerwatch/releases/latest) attaches `.deb`, `.rpm`, `.apk`, and Arch packages for `amd64` and `arm64v8`. Download the one for your distribution and architecture, then install it from the directory you saved it in.

Debian, Ubuntu, and derivatives:

```bash
sudo apt install ./clankerwatch_linux_*.deb
```

Fedora, openSUSE, and other RPM distributions:

```bash
sudo dnf install ./clankerwatch_linux_*.rpm
```

Alpine and postmarketOS:

```bash
sudo apk add --allow-untrusted ./clankerwatch_linux_*.apk
```

Arch, Manjaro, EndeavourOS, and CachyOS:

```bash
sudo pacman -U ./clankerwatch_linux_*.pkg.tar.zst
```

The pattern matches the one package you downloaded, so keep only one version in the directory.

### What a package installs

| File | Location |
| --- | --- |
| Daemon | `/usr/bin/clankerwatch` |
| systemd user unit | `/usr/lib/systemd/user/clankerwatch.service` |
| D-Bus activation file | `/usr/share/dbus-1/services/com.nickfedor.ClankerWatch1.service` |
| Widget | `/usr/share/plasma/plasmoids/com.nickfedor.clankerwatch/` |
| Example settings | `/usr/share/doc/clankerwatch/config.example.yaml` |

A package creates no settings file. Every setting has a default, and the widget's settings page creates `~/.config/clankerwatch/config.yaml` the first time you change something. To start from the documented example instead:

```bash
install -Dm600 /usr/share/doc/clankerwatch/config.example.yaml ~/.config/clankerwatch/config.yaml
```

The unit has no `[Install]` section, so there is nothing to enable. The first time the widget reads the daemon's bus name, D-Bus asks systemd to start it. If systemd does not find the new unit right away, run:

```bash
systemctl --user daemon-reload
```

### Verify a download

Each release publishes `checksums.txt`, a Sigstore bundle that signs it (`checksums.txt.sig`), and SBOMs. Check the signature of the checksums, then the checksum of your package:

```bash
cosign verify-blob \
  --bundle checksums.txt.sig \
  --certificate-identity-regexp '^https://github.com/nicholas-fedor/clankerwatch/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt
```

### Release archives

Each release also has a `.tar.gz` archive per architecture, such as `clankerwatch_linux_amd64_<version>.tar.gz`. It holds the binary, the files under `build/package/` (the unit, the D-Bus activation file, and the example settings), and the `plasmoid/` widget package, so you can place the files by hand.

## From source

Building needs Go 1.27 and [Task](https://taskfile.dev), packaged as `go-task` on Arch. Installing needs `kpackagetool6`, which ships with Plasma.

```bash
git clone https://github.com/nicholas-fedor/clankerwatch
cd clankerwatch
task install
```

`task install` builds a static binary and installs everything for your user only:

| File | Location |
| --- | --- |
| Daemon | `~/.local/bin/clankerwatch` |
| systemd user unit | `~/.config/systemd/user/clankerwatch.service` |
| D-Bus activation file | `~/.local/share/dbus-1/services/com.nickfedor.ClankerWatch1.service` |
| Settings | `~/.config/clankerwatch/config.yaml`, created from the example only when missing |
| Widget | Installed with `kpackagetool6` into your user's Plasma widgets |

Paths under `~/.config` and `~/.local/share` follow `XDG_CONFIG_HOME` and `XDG_DATA_HOME` when you set them. The unit and the activation file are rewritten to point at `~/.local/bin`. Set `BINDIR` to install the binary somewhere else. The task reloads systemd and the session bus configuration, and restarts the daemon if it was running.

After pulling new changes, `task update` reinstalls, restarts the daemon, and restarts plasmashell only when the widget's files changed, because that briefly blinks the panel.

## Add the widget to a panel

1. Right-click an empty part of a panel and choose **Add Widgets**.
2. Search for **Clanker Watch** and drag it onto the panel, or double-click it.
3. The panel shows the 5-hour session and the weekly bar as soon as the daemon has data. [Getting started](/getting-started/) explains the first run.

If Clanker Watch is missing from the list right after installing, restart plasmashell so it rescans its widgets:

```bash
systemctl --user restart plasma-plasmashell
```

The widget also works on the desktop. In a small space it falls back to the compact panel view.

## Update

Install the newer package over the old one with the same command you used to install it. Then restart the daemon so it runs the new binary, and restart plasmashell so it loads the new widget:

```bash
systemctl --user restart clankerwatch
systemctl --user restart plasma-plasmashell
```

Updating never touches your settings file or the daemon's state.

## Uninstall

Remove the widget from the panel first: right-click it and choose **Remove Clanker Watch**. Then stop the daemon and remove the package:

```bash
systemctl --user stop clankerwatch
sudo apt remove clankerwatch       # Debian and Ubuntu
sudo dnf remove clankerwatch       # Fedora and openSUSE
sudo apk del clankerwatch          # Alpine
sudo pacman -R clankerwatch        # Arch
```

For a source install, run this from the checkout:

```bash
task uninstall
```

It stops the daemon and removes the binary, the unit, the activation file, and the widget.

Neither way removes your settings or the daemon's state. Delete them to remove everything:

```bash
rm -r ~/.config/clankerwatch ~/.local/state/clankerwatch
```

Nothing in Claude Code's files changes at any point, so there is nothing to undo there.
