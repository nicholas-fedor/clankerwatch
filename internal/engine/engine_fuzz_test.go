// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzPlanName checks the plan label invariants on arbitrary login fields.
//
// The subscription and tier come from Claude Code's credentials file, so any
// text must give a label without panicking. A blank subscription gives no
// label. Otherwise the label starts with the capitalized first letter of the
// subscription, and a tier ending in a multiplier adds it at the end.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzPlanName(f *testing.F) {
	f.Add("max", "default_claude_max_5x")
	f.Add("pro", "default_claude_pro")
	f.Add("", "default_claude_max_20x")
	f.Add("  ", "")
	f.Add("équipe", "tier_12x")
	f.Add("ǆ", "x")
	f.Add("\xff\xfe", "5x")
	f.Add("1st", "007x")

	f.Fuzz(func(t *testing.T, subscription, tier string) {
		got := planName(subscription, tier)

		trimmed := strings.ToLower(strings.TrimSpace(subscription))
		if trimmed == "" {
			if got != "" {
				t.Fatalf("blank subscription %q gave %q", subscription, got)
			}

			return
		}

		first, _ := utf8.DecodeRuneInString(trimmed)
		gotFirst, _ := utf8.DecodeRuneInString(got)

		if gotFirst != unicode.ToUpper(first) {
			t.Fatalf("label %q does not start with the capitalized %q", got, first)
		}

		// Some letters uppercase to a titlecase form, such as Greek letters
		// with ypogegrammeni, so either case counts.
		if unicode.IsLetter(first) && unicode.ToUpper(first) != first &&
			!unicode.IsUpper(gotFirst) && !unicode.IsTitle(gotFirst) {
			t.Fatalf("label %q does not start with an uppercase letter", got)
		}

		if match := tierMultiplier.FindStringSubmatch(tier); match != nil && !strings.HasSuffix(got, " "+match[1]+"x") {
			t.Fatalf("label %q lacks the multiplier of tier %q", got, tier)
		}
	})
}
