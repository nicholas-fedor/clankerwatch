// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package notify decides when usage crosses an alert threshold and sends
// desktop notifications through org.freedesktop.Notifications.
//
// A Ledger remembers which thresholds were alerted for which reset window, so
// an alert is never repeated, including across daemon restarts. A bar that
// jumps past several thresholds yields one alert for the highest of them, and
// the critical alert replaces the warning in place.
package notify
