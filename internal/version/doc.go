// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package version reports build metadata for clankerwatch.
//
// Version, CommitSHA, and BuildTime are link-time variables. GetVersion never
// assigns to Version. Current returns a snapshot whose Version field is the
// GetVersion result. The constructor is named Current because a function named
// Info cannot share the package block with the type Info.
package version
