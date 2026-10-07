// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package daemon wires the engine, the D-Bus service, Claude Code's file
// readers, and the notification sender into the operations the commands run.
//
// Serve runs the real daemon, Demo replays canned states through the real
// engine, Status reports the configuration and the snapshot the daemon would
// publish without touching the network, and NotifyTest sends one sample
// notification.
package daemon
