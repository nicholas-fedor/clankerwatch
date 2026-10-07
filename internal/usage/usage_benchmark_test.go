// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// BenchmarkParse measures parsing the live payload, which runs on every poll.
func BenchmarkParse(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "live.json"))
	if err != nil {
		b.Fatal(err)
	}

	var bars int

	for b.Loop() {
		parsed, err := usage.Parse(data)
		if err != nil {
			b.Fatal(err)
		}

		bars = len(parsed.Bars)
	}

	if bars == 0 {
		b.Fatal("no bars parsed")
	}
}

// BenchmarkMoneyString measures formatting an amount, which runs on every
// panel render while extra usage is enabled.
func BenchmarkMoneyString(b *testing.B) {
	amounts := []usage.Money{
		{Currency: "USD", Minor: 1250, Decimals: 2},
		{Currency: "CHF", Minor: -123456789, Decimals: 2},
		{Currency: "JPY", Minor: 500, Decimals: 0},
	}

	var text string

	for b.Loop() {
		for _, amount := range amounts {
			text = amount.String()
		}
	}

	if text == "" {
		b.Fatal("empty amount")
	}
}
