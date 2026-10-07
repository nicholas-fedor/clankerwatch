// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// unescape reverses Escape.
var unescape = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// checkAlert fails the test unless alert is consistent with the bar and the
// thresholds it was evaluated against.
//
// Parameters:
//   - t: test handle.
//   - alert: the alert.
//   - thresholds: the thresholds, ascending.
func checkAlert(t *testing.T, alert Alert, thresholds []float64) {
	t.Helper()

	if len(alert.Covers) == 0 {
		t.Fatalf("alert covers nothing: %+v", alert)
	}

	if !slices.IsSorted(alert.Covers) {
		t.Fatalf("covers out of order: %v", alert.Covers)
	}

	if alert.Threshold != alert.Covers[len(alert.Covers)-1] {
		t.Fatalf("threshold %v is not the highest cover in %v", alert.Threshold, alert.Covers)
	}

	if alert.Top != (alert.Threshold == thresholds[len(thresholds)-1]) {
		t.Fatalf("top %v for threshold %v", alert.Top, alert.Threshold)
	}

	for _, threshold := range alert.Covers {
		if !slices.Contains(thresholds, threshold) || alert.Bar.Percent < threshold {
			t.Fatalf("cover %v invalid for %v%%", threshold, alert.Bar.Percent)
		}
	}
}

// FuzzLedgerEvaluate checks that a recorded alert is never returned again for
// the same bar window.
//
// The bar is evaluated and every alert recorded. A second evaluation at any
// percentage, with the reset time moved by up to the 30 minute tolerance, must
// not cover a threshold already recorded. The seeds cover a jump past both
// thresholds, a slow climb, a bar without resets, jitter at both edges of the
// tolerance, a drop that re-arms, and a window that already reset.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzLedgerEvaluate(f *testing.F) {
	f.Add(97.0, 99.0, int64(180), int64(0))
	f.Add(81.0, 96.0, int64(180), int64(10))
	f.Add(85.0, 90.0, int64(0), int64(0))
	f.Add(85.0, 99.0, int64(300), int64(30))
	f.Add(85.0, 99.0, int64(300), int64(-30))
	f.Add(85.0, 10.0, int64(0), int64(0))
	f.Add(100.0, 100.0, int64(-60), int64(0))
	f.Add(math.Inf(1), math.NaN(), int64(1), int64(-1))

	thresholds := []float64{50, 80, 95}

	f.Fuzz(func(t *testing.T, first, second float64, resetMinutes, jitterMinutes int64) {
		now := ledgerNow
		bar := usage.Bar{ID: "session", Label: "Current session", Percent: first}

		resetMinutes %= 7 * 24 * 60
		if resetMinutes != 0 {
			bar.ResetsAt = now.Add(time.Duration(resetMinutes) * time.Minute)
		}

		var ledger Ledger

		alerts := ledger.Evaluate(now, []usage.Bar{bar}, thresholds)
		if len(alerts) > 1 {
			t.Fatalf("%d alerts for one bar", len(alerts))
		}

		var recorded []float64

		for _, alert := range alerts {
			checkAlert(t, alert, thresholds)
			ledger.Record(alert, 1, now)

			recorded = append(recorded, alert.Covers...)
		}

		if repeat := ledger.Evaluate(now, []usage.Bar{bar}, thresholds); len(repeat) != 0 {
			t.Fatalf("alert repeated after Record: %+v", repeat)
		}

		next := bar
		next.Percent = second

		if !bar.ResetsAt.IsZero() {
			jitter := time.Duration(jitterMinutes%int64(windowTolerance/time.Minute+1)) * time.Minute
			next.ResetsAt = bar.ResetsAt.Add(jitter)
		}

		later := now.Add(time.Second)

		for _, alert := range ledger.Evaluate(later, []usage.Bar{next}, thresholds) {
			checkAlert(t, alert, thresholds)

			for _, threshold := range alert.Covers {
				if slices.Contains(recorded, threshold) {
					t.Fatalf("threshold %v alerted twice in one window", threshold)
				}
			}

			ledger.Record(alert, 2, later)
		}

		if repeat := ledger.Evaluate(later, []usage.Bar{next}, thresholds); len(repeat) != 0 {
			t.Fatalf("alert repeated after Record: %+v", repeat)
		}
	})
}

// FuzzDuration renders every duration without panicking.
//
// Every countdown ends in a unit the widget uses. The seeds cover each branch
// boundary and the extremes of time.Duration, where rounding saturates.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzDuration(f *testing.F) {
	for _, span := range []time.Duration{
		math.MinInt64, -time.Minute, 0, 59 * time.Second, time.Minute, 90 * time.Second,
		time.Hour, 72 * time.Minute, 24 * time.Hour, 47*time.Hour + 59*time.Minute, 48 * time.Hour,
		math.MaxInt64 - 1, math.MaxInt64,
	} {
		f.Add(int64(span))
	}

	f.Fuzz(func(t *testing.T, nanoseconds int64) {
		got := Duration(time.Duration(nanoseconds))

		if !strings.HasSuffix(got, " min") && !strings.HasSuffix(got, " hr") &&
			!strings.HasSuffix(got, " days") && !strings.HasSuffix(got, " minute") {
			t.Fatalf("Duration(%d) = %q", nanoseconds, got)
		}

		if strings.Contains(got, "-") {
			t.Fatalf("Duration(%d) = %q is negative", nanoseconds, got)
		}
	})
}

// FuzzEscape leaves no raw markup and loses no text.
//
// The seeds cover markup, entities that must be escaped again, a lone
// ampersand, and invalid UTF-8.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzEscape(f *testing.F) {
	f.Add("")
	f.Add("plain text")
	f.Add("<b>bold</b>")
	f.Add("&lt;already escaped&gt;")
	f.Add("&amp;amp;")
	f.Add("&")
	f.Add("\xff<\xfe>")

	f.Fuzz(func(t *testing.T, text string) {
		escaped := Escape(text)

		if strings.ContainsAny(escaped, "<>") {
			t.Fatalf("Escape(%q) = %q keeps markup", text, escaped)
		}

		if restored := unescape.Replace(escaped); restored != text {
			t.Fatalf("unescaping %q gives %q, want %q", escaped, restored, text)
		}
	})
}
