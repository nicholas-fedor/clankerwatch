// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"errors"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// FuzzParseThresholds checks the shape of every accepted threshold list.
//
// The seed corpus holds the defaults, unsorted and duplicated lists, blank
// entries, both range bounds, out-of-range and non-numeric entries, and
// exponent and hex spellings. testdata/fuzz holds "nan" as a regression seed.
// Every input either fails with an error wrapping ErrInvalidThreshold and no
// result, or yields a list that is sorted ascending, holds no duplicates, and
// keeps every value in (0, 100].
//
// Parameters:
//   - f: fuzzing handle.
func FuzzParseThresholds(f *testing.F) {
	f.Add("80,95")
	f.Add("95, 50 ,80,50")
	f.Add(",,")
	f.Add("")
	f.Add("100,0.0001")
	f.Add("0")
	f.Add("100.0000001")
	f.Add("8e1,0x1p6")
	f.Add("abc,80")

	f.Fuzz(func(t *testing.T, value string) {
		got, err := ParseThresholds(value)
		if err != nil {
			if !errors.Is(err, ErrInvalidThreshold) {
				t.Fatalf("%q: error %v does not wrap ErrInvalidThreshold", value, err)
			}

			if got != nil {
				t.Fatalf("%q: error came with thresholds %v", value, got)
			}

			return
		}

		for index, threshold := range got {
			if threshold <= 0 || threshold > maxPercent {
				t.Fatalf("%q: threshold %v outside (0, 100]", value, threshold)
			}

			if index > 0 && got[index-1] >= threshold {
				t.Fatalf("%q: %v is not strictly ascending", value, got)
			}
		}

		if strings.TrimSpace(strings.ReplaceAll(value, ",", "")) == "" && len(got) != 0 {
			t.Fatalf("%q: blank input gave thresholds %v", value, got)
		}
	})
}

// FuzzParseMode checks that only the two mode names are accepted.
//
// The seed corpus holds both modes, padded and recased spellings, and an
// unknown name. An accepted value must be one of the two modes once trimmed,
// and every rejection must wrap ErrInvalidMode.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzParseMode(f *testing.F) {
	f.Add("hybrid")
	f.Add("cache-only")
	f.Add(" hybrid\t")
	f.Add("HYBRID")
	f.Add("network")

	f.Fuzz(func(t *testing.T, value string) {
		got, err := ParseMode(value)
		if err != nil {
			if !errors.Is(err, ErrInvalidMode) || got != "" {
				t.Fatalf("%q: got %q with error %v", value, got, err)
			}

			return
		}

		if got != ModeHybrid && got != ModeCacheOnly {
			t.Fatalf("%q: accepted unknown mode %q", value, got)
		}

		if string(got) != strings.TrimSpace(value) {
			t.Fatalf("%q: parsed as %q", value, got)
		}
	})
}

// FuzzDecodeFile checks the strict settings decoder on arbitrary content.
//
// The seed corpus holds the shipped example, a file setting every key, an
// empty list, a comments-only file, a second document, an unknown key, a
// duplicate key, and malformed YAML. Decoding must never panic. A failure
// wraps ErrInvalidFile and returns no settings. A success applied to valid
// defaults either succeeds or fails with one of the value errors.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzDecodeFile(f *testing.F) {
	example, err := os.ReadFile(exampleFile)
	if err != nil {
		f.Fatal(err)
	}

	f.Add(example)
	f.Add([]byte("mode: cache-only\ninterval: 3m\nidleInterval: 1h\nnotify: [50, 95]\nnotifyAuth: false\n" +
		"logLevel: debug\nclaudeConfigDir: /c\nstateDir: /s\n"))
	f.Add([]byte("notify: []\n"))
	f.Add([]byte("# comments only\n"))
	f.Add([]byte("interval: 5m\n---\ninterval: 9m\n"))
	f.Add([]byte("bogus: true\n"))
	f.Add([]byte("mode: a\nmode: b\n"))
	f.Add([]byte("notify: [80\n"))
	f.Add([]byte("notify: [.nan, -.inf]\nlogLevel: [x]\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := DecodeFile(data)
		if err != nil {
			if !errors.Is(err, ErrInvalidFile) {
				t.Fatalf("error %v does not wrap ErrInvalidFile", err)
			}

			if file != (File{}) {
				t.Fatalf("error %v came with settings %+v", err, file)
			}

			return
		}

		root := t.TempDir()
		cfg := Default(Dirs{Home: root, StateHome: root, ConfigHome: root})

		err = file.Apply(&cfg)
		if err != nil && !errors.Is(err, ErrInvalidMode) && !errors.Is(err, ErrInvalidThreshold) &&
			!errors.Is(err, ErrInvalidLogLevel) {
			t.Fatalf("apply error %v wraps no value error", err)
		}
	})
}

// fuzzModes are the mode values FuzzUpdateFile picks from, including strings
// YAML would read as another type or as structure unless quoted.
var fuzzModes = []string{"hybrid", "cache-only", "", "null", "~", "true", "5m", "a: b", "# x", "- x", "'", "\n"}

// FuzzUpdateFile checks that an update sets exactly the patched keys.
//
// The seed corpus holds the shipped example, a mapping with head, line, and
// trailing comments, a block-style notify list, empty content, and malformed
// YAML. The fuzzer varies the content, a patch interval in minutes, a notify
// threshold, notifyAuth, a mode from fuzzModes, and a mask of which of those
// four keys the patch sets. Content DecodeFile refuses must fail with an
// error wrapping ErrInvalidFile. Any other content must update, decode, hold
// every patched key with its patched value, and keep every other key.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzUpdateFile(f *testing.F) {
	example, err := os.ReadFile(exampleFile)
	if err != nil {
		f.Fatal(err)
	}

	f.Add(example, uint16(3), 50.0, false, uint8(1), uint8(0xf))
	f.Add(example, uint16(0), 80.0, true, uint8(0), uint8(0))
	f.Add([]byte("# head\nmode: hybrid # line\ninterval: 5m\n\n# trailing\n"), uint16(10), 95.0, true, uint8(0),
		uint8(0x5))
	f.Add([]byte("notify:\n  - 50\n  - 60\nlogLevel: warn\n"), uint16(90), 12.5, false, uint8(3), uint8(0x2))
	f.Add([]byte(""), uint16(2), 100.0, true, uint8(4), uint8(0xf))
	f.Add([]byte("notify: [80\n"), uint16(5), 80.0, true, uint8(0), uint8(0xf))
	f.Add([]byte("bogus: 1\n"), uint16(5), 80.0, true, uint8(0), uint8(0x1))

	f.Fuzz(func(t *testing.T, data []byte, minutes uint16, threshold float64, notifyAuth bool, mode, mask uint8) {
		patch := fuzzPatch(minutes, threshold, notifyAuth, mode, mask)

		before, decodeErr := DecodeFile(data)

		out, err := UpdateFile(data, patch)
		if decodeErr != nil {
			if !errors.Is(err, ErrInvalidFile) || out != nil {
				t.Fatalf("%q: undecodable content gave %q with error %v", data, out, err)
			}

			return
		}

		if err != nil {
			t.Fatalf("%q with %+v: update failed: %v", data, patch, err)
		}

		after, err := DecodeFile(out)
		if err != nil {
			t.Fatalf("%q: update gave undecodable %q: %v", data, out, err)
		}

		want := overlay(before, patch)
		if !sameFile(want, after) {
			t.Fatalf("%q with %+v gave %q, decoded %+v", data, patch, out, after)
		}
	})
}

// fuzzPatch builds a patch from fuzzed values.
//
// Parameters:
//   - minutes: the interval in minutes.
//   - threshold: the only notify threshold.
//   - notifyAuth: the login alert setting.
//   - mode: an index into fuzzModes, wrapped.
//   - mask: bits 0 to 3 select interval, notify, notifyAuth, and mode.
//
// Returns:
//   - File: the patch.
func fuzzPatch(minutes uint16, threshold float64, notifyAuth bool, mode, mask uint8) File {
	var patch File

	if mask&1 != 0 {
		patch.Interval = new(time.Duration(minutes) * time.Minute)
	}

	if mask&2 != 0 {
		patch.Notify = new([]float64{threshold})
	}

	if mask&4 != 0 {
		patch.NotifyAuth = new(notifyAuth)
	}

	if mask&8 != 0 {
		patch.Mode = new(fuzzModes[int(mode)%len(fuzzModes)])
	}

	return patch
}

// overlay returns base with every present key of patch set.
//
// Parameters:
//   - base: the settings before the update.
//   - patch: the settings to set.
//
// Returns:
//   - File: the settings an update must produce.
func overlay(base, patch File) File {
	target := reflect.ValueOf(&base).Elem()
	source := reflect.ValueOf(patch)

	for index := range source.NumField() {
		if !source.Field(index).IsNil() {
			target.Field(index).Set(source.Field(index))
		}
	}

	return base
}

// sameFile reports whether two decoded files hold the same settings, with NaN
// thresholds equal to each other.
//
// Parameters:
//   - want: the expected settings.
//   - got: the decoded settings.
//
// Returns:
//   - bool: true when every key matches.
func sameFile(want, got File) bool {
	if (want.Notify == nil) != (got.Notify == nil) {
		return false
	}

	if want.Notify != nil && !slices.EqualFunc(*want.Notify, *got.Notify, sameFloat) {
		return false
	}

	want.Notify, got.Notify = nil, nil

	return reflect.DeepEqual(want, got)
}

// sameFloat reports whether two thresholds are equal, treating NaN as equal
// to NaN.
//
// Parameters:
//   - want: the expected threshold.
//   - got: the decoded threshold.
//
// Returns:
//   - bool: true when both are equal or both are NaN.
func sameFloat(want, got float64) bool {
	return want == got || math.IsNaN(want) && math.IsNaN(got)
}
