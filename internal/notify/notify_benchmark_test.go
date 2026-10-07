// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// BenchmarkLedgerEvaluate measures one poll over a full set of bars whose
// thresholds were all alerted already, the steady state of a busy week.
func BenchmarkLedgerEvaluate(b *testing.B) {
	now := time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)
	thresholds := []float64{50, 80, 95}

	bars := make([]usage.Bar, 0, 8)
	for i := range 6 {
		bars = append(bars, usage.Bar{
			ID:       "weekly_scoped:model:" + strconv.Itoa(i),
			Percent:  97,
			ResetsAt: now.Add(time.Duration(i+1) * 24 * time.Hour),
		})
	}

	bars = append(bars,
		usage.Bar{ID: "unlimited", Percent: 97},
		usage.Bar{ID: "credit", Percent: 100, Credit: true},
	)

	var ledger notify.Ledger

	for _, alert := range ledger.Evaluate(now, bars, thresholds) {
		ledger.Record(alert, 1, now)
	}

	var alerts []notify.Alert

	for b.Loop() {
		alerts = ledger.Evaluate(now, bars, thresholds)
	}

	if len(alerts) != 0 {
		b.Fatalf("got %d repeated alerts", len(alerts))
	}
}
