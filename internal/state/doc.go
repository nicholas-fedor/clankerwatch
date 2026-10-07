// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package state persists what the daemon must remember across restarts.
//
// That is the last good usage payload, the backoff and auth block that keep a
// restart from ignoring a Retry-After, and the alerts already delivered. The
// state never contains the token. The File store replaces its file through a
// temporary file and a rename, so a crash never leaves it half written.
package state
