// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Message is one desktop notification.
type Message struct {
	// Summary is the plain-text title.
	Summary string

	// Body may contain the specification's basic markup, so text is escaped.
	Body string

	// Icon is a freedesktop icon name.
	Icon string

	// Urgency is 0 for low, 1 for normal, or 2 for critical.
	Urgency byte

	// ExpireMs is -1 for the server default or 0 for never.
	ExpireMs int32

	// ReplacesID is the notification this one replaces, or 0.
	ReplacesID uint32
}

const (
	// urgencyNormal is the specification's normal urgency.
	urgencyNormal byte = 1

	// urgencyCritical is the specification's critical urgency.
	urgencyCritical byte = 2

	// expireDefault leaves expiry to the server.
	expireDefault int32 = -1

	// expireNever keeps the notification until it is dismissed.
	expireNever int32 = 0

	// limitReached is the percentage at which a limit is exhausted.
	limitReached = 100

	// minutesPerDay is the number of minutes in a day.
	minutesPerDay = 24 * 60

	// minutesPerHour is the number of minutes in an hour.
	minutesPerHour = 60

	// hoursPerDay is the number of hours in a day.
	hoursPerDay = 24

	// pluralDays is the day count from which a countdown shows whole days only.
	pluralDays = 2

	// iconWarning is the icon for a lower threshold.
	iconWarning = "dialog-warning"

	// iconCritical is the icon for the highest threshold.
	iconCritical = "dialog-error"
)

// markup escapes the characters the notification body treats as markup.
var markup = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Format builds the notification for a threshold alert.
//
// The highest threshold is critical and does not expire. Lower thresholds use
// normal urgency and the server's default expiry.
//
// Parameters:
//   - alert: the alert.
//   - now: the current time, for the reset countdown.
//
// Returns:
//   - Message: the notification without a replacement ID.
func Format(alert Alert, now time.Time) Message {
	bar := alert.Bar

	summary := fmt.Sprintf("%s at %.0f%%", bar.Label, math.Floor(bar.Percent))
	if bar.Percent >= limitReached {
		summary = bar.Label + ": limit reached"
	}

	body := ""

	if !bar.ResetsAt.IsZero() {
		left := Duration(bar.ResetsAt.Sub(now))

		body = fmt.Sprintf("Resets in %s (%s)", left, clock(bar.ResetsAt.Local(), now.Local()))
	}

	message := Message{
		Summary:    summary,
		Body:       Escape(body),
		Icon:       iconWarning,
		Urgency:    urgencyNormal,
		ExpireMs:   expireDefault,
		ReplacesID: 0,
	}
	if alert.Top {
		message.Icon, message.Urgency, message.ExpireMs = iconCritical, urgencyCritical, expireNever
	}

	return message
}

// AuthMessage is the notification for a login that needs attention.
//
// Returns:
//   - Message: the notification.
func AuthMessage() Message {
	return Message{
		Summary:    "Claude Code login needs attention",
		Body:       Escape("Usage bars are paused. Run claude and sign in with /login."),
		Icon:       "dialog-password",
		Urgency:    urgencyNormal,
		ExpireMs:   expireDefault,
		ReplacesID: 0,
	}
}

// Duration renders a countdown such as "1 hr 12 min" or "6 days".
//
// Parameters:
//   - span: the time left.
//
// Returns:
//   - string: the countdown, matching the widget's wording.
func Duration(span time.Duration) string {
	if span < time.Minute {
		return "less than a minute"
	}

	total := int(span.Round(time.Minute) / time.Minute)
	days, hours, minutes := total/minutesPerDay, total/minutesPerHour%hoursPerDay, total%minutesPerHour

	switch {
	case days >= pluralDays:
		return fmt.Sprintf("%d days", days)
	case days == 1:
		return fmt.Sprintf("1 day %d hr", hours)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%d hr %d min", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%d hr", hours)
	default:
		return fmt.Sprintf("%d min", minutes)
	}
}

// Escape protects text placed in a notification body from markup parsing.
//
// Parameters:
//   - text: plain text.
//
// Returns:
//   - string: text with &, <, and > escaped.
func Escape(text string) string { return markup.Replace(text) }

// clock renders a reset time for the alert body.
//
// Parameters:
//   - moment: the reset time.
//   - now: the current time.
//
// Returns:
//   - string: "3:20 PM" today, or "Wed 3:20 PM" on another day.
func clock(moment, now time.Time) string {
	year, month, day := moment.Date()
	nowYear, nowMonth, nowDay := now.Date()

	if year == nowYear && month == nowMonth && day == nowDay {
		return moment.Format("3:04 PM")
	}

	return moment.Format("Mon 3:04 PM")
}
