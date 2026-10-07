// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// number accepts a JSON number, a numeric string, or null.
type number struct {
	value float64
	ok    bool
}

// window is one flat usage window, such as five_hour.
//
//nolint:tagliatelle // the usage API uses snake_case keys.
type window struct {
	ResetsAt    *string `json:"resets_at"`
	Utilization number  `json:"utilization"`
}

// named is a scope target with an ID and a display name.
//
//nolint:tagliatelle // the usage API uses snake_case keys.
type named struct {
	ID          *string `json:"id"`
	DisplayName string  `json:"display_name"`
}

// limitScope narrows a limit to a model or a surface.
type limitScope struct {
	Model   *named          `json:"model"`
	Surface json.RawMessage `json:"surface"`
}

// limitRow is one entry of the server's limits list.
//
//nolint:tagliatelle // the usage API uses snake_case keys.
type limitRow struct {
	ResetsAt *string     `json:"resets_at"`
	Scope    *limitScope `json:"scope"`
	Kind     string      `json:"kind"`
	Group    string      `json:"group"`
	Severity string      `json:"severity"`
	Percent  number      `json:"percent"`
	IsActive bool        `json:"is_active"`
}

// extraUsage is the pay-as-you-go section.
//
//nolint:tagliatelle // the usage API uses snake_case keys.
type extraUsage struct {
	Currency          *string `json:"currency"`
	DecimalPlaces     *int    `json:"decimal_places"`
	DisabledReason    *string `json:"disabled_reason"`
	MonthlyLimit      number  `json:"monthly_limit"`
	UsedCredits       number  `json:"used_credits"`
	Utilization       number  `json:"utilization"`
	IsEnabled         bool    `json:"is_enabled"`
	SpendLimitReached bool    `json:"spend_limit_reached"`
}

// payload lists the keys the parser uses. Every other key is ignored.
//
//nolint:tagliatelle // the usage API uses snake_case keys.
type payload struct {
	FiveHour       *window     `json:"five_hour"`
	SevenDay       *window     `json:"seven_day"`
	SevenDaySonnet *window     `json:"seven_day_sonnet"`
	SevenDayOpus   *window     `json:"seven_day_opus"`
	CinderCove     *window     `json:"cinder_cove"`
	ExtraUsage     *extraUsage `json:"extra_usage"`
	Limits         []limitRow  `json:"limits"`
}

// jsonNull is the JSON null literal.
const jsonNull = "null"

// UnmarshalJSON decodes a number, a numeric string, or null.
//
// A string that is not a finite number decodes as unknown rather than failing
// the whole payload.
//
// Parameters:
//   - data: the JSON value.
//
// Returns:
//   - error: a decode error for a malformed string literal.
func (n *number) UnmarshalJSON(data []byte) error {
	*n = number{value: 0, ok: false}

	text := strings.TrimSpace(string(data))
	if text == jsonNull {
		return nil
	}

	if strings.HasPrefix(text, `"`) {
		var quoted string

		err := json.Unmarshal(data, &quoted)
		if err != nil {
			return fmt.Errorf("decode numeric string: %w", err)
		}

		text = strings.TrimSpace(quoted)
	}

	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil //nolint:nilerr // an unparsable number is treated as unknown.
	}

	*n = number{value: value, ok: true}

	return nil
}

// name returns the display name, or the ID when there is no display name.
//
// Returns:
//   - string: the name, or empty for a nil target.
func (n *named) name() string {
	if n == nil {
		return ""
	}

	if n.DisplayName != "" {
		return n.DisplayName
	}

	if n.ID != nil {
		return *n.ID
	}

	return ""
}

// parseTime parses an RFC 3339 timestamp.
//
// Parameters:
//   - value: the timestamp, or nil.
//
// Returns:
//   - [time.Time]: the time, or zero for a missing or malformed value.
func parseTime(value *string) time.Time {
	if value == nil || *value == "" {
		return time.Time{}
	}

	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

// surfaceName reads a surface scope given as a string or as a named object.
//
// Parameters:
//   - raw: the surface value.
//
// Returns:
//   - string: the surface name, or empty.
func surfaceName(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == jsonNull {
		return ""
	}

	var text string

	if json.Unmarshal(raw, &text) == nil {
		return text
	}

	var target named

	if json.Unmarshal(raw, &target) == nil {
		return target.name()
	}

	return ""
}
