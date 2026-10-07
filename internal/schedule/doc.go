// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package schedule decides when the daemon calls the usage endpoint.
//
// Decide is a pure function of Inputs, so every rule is covered by table
// tests. Gates come first: a missing or unusable login, a token the server
// rejected, a token about to expire, and a running backoff all block
// fetching. Without a gate, the next fetch is due one interval after the last
// data or attempt, earlier when a window resets, and immediately for an
// allowed widget refresh.
//
// Backoff grows per failure kind. Rate limits honor the server's Retry-After
// with a five-minute floor and a one-hour cap.
package schedule
