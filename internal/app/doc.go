// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package app is the composition root.
//
// Run wires the logger, the XDG directories, and the daemon operations into
// the command tree, executes it, and maps its error to a process status.
package app
