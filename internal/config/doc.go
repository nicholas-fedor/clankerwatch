// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config holds the daemon settings and their validation.
//
// A Config is built from defaults, then overridden by environment variables
// and command-line flags in the cmd/options package. Directories follow the
// XDG Base Directory specification through Dirs, which SystemDirs fills from
// the environment. Validate enforces the polling floor, so a misconfigured
// daemon refuses to start instead of hammering the usage endpoint.
package config
