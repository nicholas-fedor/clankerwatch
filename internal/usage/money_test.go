// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMoneyString covers symbols, codes, signs, and decimal placement.
func TestMoneyString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		want  string
		money Money
	}{
		{name: "dollars", money: Money{Currency: "USD", Minor: 1250, Decimals: 2}, want: "$12.50"},
		{name: "euros", money: Money{Currency: "EUR", Minor: 1250, Decimals: 2}, want: "€12.50"},
		{name: "pounds", money: Money{Currency: "GBP", Minor: 7, Decimals: 2}, want: "£0.07"},
		{name: "yen without decimals", money: Money{Currency: "JPY", Minor: 500, Decimals: 0}, want: "¥500"},
		{name: "lowercase code gets a symbol", money: Money{Currency: "usd", Minor: 100, Decimals: 2}, want: "$1.00"},
		{name: "unknown code trails", money: Money{Currency: "CHF", Minor: 123456, Decimals: 2}, want: "1234.56 CHF"},
		{name: "unknown code is uppercased", money: Money{Currency: "chf", Minor: 1, Decimals: 2}, want: "0.01 CHF"},
		{name: "no currency", money: Money{Minor: 1999, Decimals: 2}, want: "19.99"},
		{name: "zero", money: Money{Currency: "USD", Minor: 0, Decimals: 2}, want: "$0.00"},
		{name: "zero without decimals", money: Money{Minor: 0, Decimals: 0}, want: "0"},
		{name: "negative before symbol", money: Money{Currency: "USD", Minor: -5, Decimals: 2}, want: "-$0.05"},
		{name: "negative with code", money: Money{Currency: "CHF", Minor: -150, Decimals: 2}, want: "-1.50 CHF"},
		{name: "digits equal to decimals", money: Money{Minor: 99, Decimals: 2}, want: "0.99"},
		{name: "three decimals", money: Money{Minor: 1234, Decimals: 3}, want: "1.234"},
		{name: "negative decimals clamp to zero", money: Money{Minor: 42, Decimals: -3}, want: "42"},
		{name: "large decimals clamp to nine", money: Money{Minor: 1, Decimals: 40}, want: "0.000000001"},
		{
			name:  "largest amount",
			money: Money{Currency: "USD", Minor: math.MaxInt64, Decimals: 2},
			want:  "$92233720368547758.07",
		},
		{
			name:  "most negative amount",
			money: Money{Currency: "USD", Minor: math.MinInt64, Decimals: 2},
			want:  "-$92233720368547758.08",
		},
		{
			name:  "most negative amount without decimals",
			money: Money{Minor: math.MinInt64, Decimals: 0},
			want:  "-9223372036854775808",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.money.String())
		})
	}
}
