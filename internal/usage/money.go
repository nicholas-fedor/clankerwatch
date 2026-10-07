// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"strconv"
	"strings"
)

// Money is an amount in the minor units of a currency.
type Money struct {
	// Currency is the ISO 4217 code, which may be empty.
	Currency string

	// Minor is the amount in minor units, such as cents.
	Minor int64

	// Decimals is the number of minor-unit digits.
	Decimals int
}

// maxDecimals bounds the decimal places formatted for an amount.
const maxDecimals = 9

// currencySymbols maps ISO 4217 codes to the symbols placed before amounts.
var currencySymbols = map[string]string{
	"USD": "$",
	"EUR": "€",
	"GBP": "£",
	"JPY": "¥",
}

// String formats the amount with integer arithmetic, so no float rounding
// leaks into the displayed value.
//
// Known currencies get a leading symbol, others a trailing code.
//
// Returns:
//   - string: the formatted amount, such as €12.50 or 1234.56 CHF.
func (m Money) String() string {
	sign := "" // Unsigned negation of the two's complement value is the magnitude, which.
	// also holds for the most negative amount.
	magnitude := uint64(m.Minor) //nolint:gosec // The sign is restored below.
	if m.Minor < 0 {
		sign = "-"
		magnitude = -magnitude
	}

	decimals := min(max(m.Decimals, 0), maxDecimals)

	digits := strconv.FormatUint(magnitude, 10)
	if decimals > 0 {
		if len(digits) <= decimals {
			digits = strings.Repeat("0", decimals-len(digits)+1) + digits
		}

		digits = digits[:len(digits)-decimals] + "." + digits[len(digits)-decimals:]
	}

	code := strings.ToUpper(m.Currency)
	if symbol, ok := currencySymbols[code]; ok {
		return sign + symbol + digits
	}

	if code != "" {
		return sign + digits + " " + code
	}

	return sign + digits
}
