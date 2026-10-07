// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package cmd assembles the clankerwatch command tree.
//
// Each subcommand lives in its own package and depends on the narrowest
// interface it can use. Settings are flags on the root command whose
// defaults come from environment variables, and a command resolves them only
// when it needs them, so the version command works even with a broken
// environment file.
package cmd
