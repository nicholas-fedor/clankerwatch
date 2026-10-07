// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"testing"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
)

// BenchmarkBuildSnapshot measures rendering the snapshot on every wake.
//
// The engine builds the snapshot on each wake to compare it with the last
// one, so its cost is paid even when nothing is published.
//
// Parameters:
//   - b: benchmark handle.
func BenchmarkBuildSnapshot(b *testing.B) {
	e := newTestEngine(b, testOptions(), Deps{})
	e.oauth = testLogin(testNow.Add(time.Hour))
	withData(b, e, testPayload(b, 85, 40, testNow.Add(time.Hour)), testNow)
	e.decision = schedule.Decision{NextFetchAt: testNow.Add(5 * time.Minute), RefreshAllowedAt: testNow.Add(2 * time.Minute)}

	var sink string

	b.ReportAllocs()

	for b.Loop() {
		sink = e.buildSnapshot()
	}

	if sink == "" {
		b.Fatal("empty snapshot")
	}
}
