// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bus publishes the daemon's snapshot on the D-Bus session bus.
//
// The object exports a Snapshot property, which carries one JSON document so
// updates are atomic and QML never decodes container types, a constant
// Version property, and a Refresh method. Everything is exported before the
// bus name is claimed, so a widget that activated the daemon never sees a
// half-built object. An unchanged snapshot emits no signal, and properties
// are always sent by value, because the Plasma QML binding drops invalidated
// properties.
package bus
