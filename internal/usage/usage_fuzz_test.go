// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// amountPart strips the sign, symbol, and code from a formatted amount.
//
// Parameters:
//   - t: test handle.
//   - m: the amount that was formatted.
//   - text: the result of m.String().
//
// Returns:
//   - string: the digits and decimal point alone.
func amountPart(t *testing.T, m Money, text string) string {
	t.Helper()

	text = strings.TrimPrefix(text, "-")
	code := strings.ToUpper(m.Currency)

	if symbol, ok := currencySymbols[code]; ok {
		if !strings.HasPrefix(text, symbol) {
			t.Fatalf("%q lacks symbol %q", text, symbol)
		}

		return strings.TrimPrefix(text, symbol)
	}

	if code != "" {
		if !strings.HasSuffix(text, " "+code) {
			t.Fatalf("%q lacks code %q", text, code)
		}

		return strings.TrimSuffix(text, " "+code)
	}

	return text
}

// magnitudeOf returns the absolute value of an amount without overflow.
//
// Parameters:
//   - minor: the amount in minor units.
//
// Returns:
//   - uint64: its magnitude, including for math.MinInt64.
func magnitudeOf(minor int64) uint64 {
	if minor < 0 {
		return uint64(-(minor + 1)) + 1
	}

	return uint64(minor)
}

// FuzzParse checks the structural invariants of parsed usage.
//
// The seed corpus holds the live fixture, a flat-window payload, a limits
// payload with scoped and unknown kinds, a credit window, an extra usage
// section with extreme amounts, a null known key, and malformed JSON. Every
// payload must parse without panicking, and every bar must have a non-empty
// ID that no other bar shares and a finite percentage.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzParse(f *testing.F) {
	live, err := os.ReadFile(filepath.Join("testdata", liveFixture))
	if err != nil {
		f.Fatal(err)
	}

	f.Add(live)
	f.Add([]byte(`{"five_hour":{"utilization":"12"},"seven_day":{"utilization":40},"seven_day_opus":{"utilization":3}}`))
	f.Add([]byte(`{"limits":[{"kind":"weekly_scoped","scope":{"surface":{"id":"cc"}}},{"kind":"monthly_all","percent":"1e2"}]}`))
	f.Add([]byte(`{"limits":[{"kind":"session"},{"kind":"session"}],"cinder_cove":{"utilization":5}}`))
	f.Add([]byte(`{"extra_usage":{"is_enabled":true,"used_credits":-9.3e18,"monthly_limit":9.2e18,"decimal_places":-4}}`))
	f.Add([]byte(`{"seven_day_sonnet":null}`))
	f.Add([]byte(`{"limits":[`))

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := Parse(data)
		if err != nil {
			if len(got.Bars) != 0 || got.Extra != nil {
				t.Fatalf("error %v came with a result %+v", err, got)
			}

			return
		}

		seen := make(map[string]bool, len(got.Bars))

		for _, bar := range got.Bars {
			if bar.ID == "" {
				t.Fatalf("bar without an ID: %+v", bar)
			}

			if seen[bar.ID] {
				t.Fatalf("duplicate bar ID %q", bar.ID)
			}

			seen[bar.ID] = true

			if math.IsNaN(bar.Percent) || math.IsInf(bar.Percent, 0) {
				t.Fatalf("bar %q has percent %v", bar.ID, bar.Percent)
			}
		}

		if got.Extra != nil {
			if got.Extra.Percent != nil && (math.IsNaN(*got.Extra.Percent) || math.IsInf(*got.Extra.Percent, 0)) {
				t.Fatalf("extra percent %v", *got.Extra.Percent)
			}

			for _, amount := range []*Money{got.Extra.Used, got.Extra.Limit} {
				if amount != nil {
					_ = amount.String()
				}
			}
		}
	})
}

// FuzzMoneyString checks the formatted amount against its integer value.
//
// The seed corpus holds ordinary amounts, zero, the extreme int64 values
// (math.MinInt64 has no positive counterpart, so its magnitude is the
// classic overflow trap), out-of-range decimals, and an unknown code that
// itself contains a dot. The sign prefix must appear exactly for negative
// amounts. With Decimals in 1..9 the digits hold exactly one '.' followed by
// that many digits. Removing the dot must give back the magnitude.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzMoneyString(f *testing.F) {
	f.Add(int64(1250), 2, "USD")
	f.Add(int64(0), 0, "")
	f.Add(int64(-5), 2, "eur")
	f.Add(int64(math.MaxInt64), 9, "JPY")
	f.Add(int64(math.MinInt64), 2, "USD")
	f.Add(int64(math.MinInt64), 0, "")
	f.Add(int64(7), -3, "CHF")
	f.Add(int64(7), 40, "A.B")

	f.Fuzz(func(t *testing.T, minor int64, decimals int, currency string) {
		amount := Money{Minor: minor, Decimals: decimals, Currency: currency}
		text := amount.String()

		if strings.HasPrefix(text, "-") != (minor < 0) {
			t.Fatalf("%q: sign does not match %d", text, minor)
		}

		digits := amountPart(t, amount, text)
		effective := min(max(decimals, 0), maxDecimals)

		if effective == 0 {
			if strings.Contains(digits, ".") {
				t.Fatalf("%q: unexpected decimal point", digits)
			}
		} else {
			if strings.Count(digits, ".") != 1 {
				t.Fatalf("%q: want exactly one decimal point", digits)
			}

			whole, fraction, _ := strings.Cut(digits, ".")
			if whole == "" || len(fraction) != effective {
				t.Fatalf("%q: want %d fraction digits after a whole part", digits, effective)
			}
		}

		parsed, err := strconv.ParseUint(strings.Replace(digits, ".", "", 1), 10, 64)
		if err != nil {
			t.Fatalf("%q: digits do not parse: %v", digits, err)
		}

		if parsed != magnitudeOf(minor) {
			t.Fatalf("%q: magnitude %d, want %d", digits, parsed, magnitudeOf(minor))
		}
	})
}

// FuzzParseRetryAfter checks the bounds on a server-requested delay.
//
// The seed corpus holds delta seconds at and past the cap, zero, negative,
// out-of-range and fractional values, and HTTP dates in the past and future
// relative to a fixed clock. A delay is never negative and never above 24
// hours, and a value that is not honored reports a zero delay.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzParseRetryAfter(f *testing.F) {
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

	f.Add("30")
	f.Add("0")
	f.Add("-1")
	f.Add("86401")
	f.Add("99999999999999999999")
	f.Add("1.5")
	f.Add(now.Add(time.Hour).Format(http.TimeFormat))
	f.Add(now.Add(-time.Hour).Format(http.TimeFormat))
	f.Add("Mon, 01 Jan 9999 00:00:00 GMT")

	f.Fuzz(func(t *testing.T, value string) {
		delay, ok := ParseRetryAfter(value, now)

		if delay < 0 || delay > maxRetryAfter {
			t.Fatalf("%q: delay %v out of bounds", value, delay)
		}

		if !ok && delay != 0 {
			t.Fatalf("%q: rejected but delay is %v", value, delay)
		}

		if ok && delay == 0 {
			t.Fatalf("%q: accepted with no delay", value)
		}
	})
}

// FuzzNumberUnmarshal checks the lenient numeric decoder.
//
// The seed corpus holds bare and quoted numbers, null, NaN and infinity
// spellings, an overflowing exponent, a broken escape, and a non-number. A
// known value is always finite, an error always leaves the value unknown, and
// any input other than null that encoding/json accepts as a float64 decodes to the same value.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzNumberUnmarshal(f *testing.F) {
	f.Add([]byte(`6`))
	f.Add([]byte(`-12.5e1`))
	f.Add([]byte(`"42"`))
	f.Add([]byte(`null`))
	f.Add([]byte(`"NaN"`))
	f.Add([]byte(`"-Inf"`))
	f.Add([]byte(`1e400`))
	f.Add([]byte(`"\q"`))
	f.Add([]byte(`true`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var got number

		err := got.UnmarshalJSON(data)
		if err != nil && got.ok {
			t.Fatalf("%q: error %v with a known value", data, err)
		}

		if got.ok && (math.IsNaN(got.value) || math.IsInf(got.value, 0)) {
			t.Fatalf("%q: known value %v is not finite", data, got.value)
		}

		// encoding/json accepts null into a float64 as a no-op, which this
		// decoder reports as unknown instead.
		if string(bytes.TrimSpace(data)) == jsonNull {
			return
		}

		var want float64

		if json.Unmarshal(data, &want) == nil {
			if !got.ok || got.value != want {
				t.Fatalf("%q: got %v (ok=%v), want %v", data, got.value, got.ok, want)
			}
		}
	})
}
