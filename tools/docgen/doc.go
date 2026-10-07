// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command docgen writes the clankerwatch CLI reference as Hugo pages.
//
// It reads the command tree from [cmd.DocRoot], which builds the same tree the
// binary runs without any process dependencies. No command is executed, so
// docgen never touches the network, D-Bus, or the user's files, and the
// reference cannot drift from the shipped command line.
//
// Commands and flags are visited in sorted order, so the same tree always
// produces the same bytes.
//
// Usage:
//
//	go run ./tools/docgen -out ./docs/content/cli-reference
package main
