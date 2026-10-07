// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package engine decides when to fetch usage, keeps the latest data and
// failure state, and builds the snapshot the widget displays.
//
// One goroutine owns all state. Each wake reads Claude Code's files, asks the
// pure Decide function whether to fetch, fetches when told to, evaluates
// alerts, publishes the snapshot when its bytes changed, and saves state when
// anything persisted changed. Every decision uses wall-clock time and every
// sleep is capped, because Go timers stop while the machine is suspended.
//
// The engine never refreshes a token. When Claude Code's token is about to
// expire, it waits for Claude Code to refresh it.
package engine
