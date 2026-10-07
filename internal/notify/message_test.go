// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// messageNow is a Wednesday morning in the local zone, so the rendered clock
// matches the literals below wherever the tests run.
var messageNow = time.Date(2026, time.July, 15, 10, 0, 0, 0, time.Local)

// TestFormat covers the summary, the countdown, and the urgency.
func TestFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want  Message
		name  string
		alert Alert
	}{
		{
			name: "warning today",
			alert: Alert{
				Bar:       usage.Bar{Label: "Current session", Percent: 82.9, ResetsAt: messageNow.Add(72 * time.Minute)},
				Threshold: 80,
			},
			want: Message{
				Summary:  "Current session at 82%",
				Body:     "Resets in 1 hr 12 min (11:12 AM)",
				Icon:     "dialog-warning",
				Urgency:  1,
				ExpireMs: -1,
			},
		},
		{
			name: "critical on another day",
			alert: Alert{
				Bar:       usage.Bar{Label: "Current week (all models)", Percent: 96, ResetsAt: messageNow.Add(26 * time.Hour)},
				Threshold: 95,
				Top:       true,
			},
			want: Message{
				Summary:  "Current week (all models) at 96%",
				Body:     "Resets in 1 day 2 hr (Thu 12:00 PM)",
				Icon:     "dialog-error",
				Urgency:  2,
				ExpireMs: 0,
			},
		},
		{
			name:  "limit reached",
			alert: Alert{Bar: usage.Bar{Label: "Current session", Percent: 100}, Threshold: 95, Top: true},
			want: Message{
				Summary:  "Current session: limit reached",
				Icon:     "dialog-error",
				Urgency:  2,
				ExpireMs: 0,
			},
		},
		{
			name:  "no reset time",
			alert: Alert{Bar: usage.Bar{Label: "Fable <beta>", Percent: 80}, Threshold: 80},
			want: Message{
				Summary:  "Fable <beta> at 80%",
				Icon:     "dialog-warning",
				Urgency:  1,
				ExpireMs: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Format(tt.alert, messageNow))
		})
	}
}

// TestFormatUsesLocalTime renders the reset clock in the local zone whatever
// zone the inputs carry.
func TestFormatUsesLocalTime(t *testing.T) {
	t.Parallel()

	alert := Alert{Bar: usage.Bar{Label: "Current session", Percent: 85, ResetsAt: messageNow.Add(72 * time.Minute).UTC()}}

	assert.Equal(t, "Resets in 1 hr 12 min (11:12 AM)", Format(alert, messageNow.UTC()).Body)
}

// TestAuthMessage asks the user to sign in again.
func TestAuthMessage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Message{
		Summary:  "Claude Code login needs attention",
		Body:     "Usage bars are paused. Run claude and sign in with /login.",
		Icon:     "dialog-password",
		Urgency:  1,
		ExpireMs: -1,
	}, AuthMessage())
}

// TestDuration matches the widget's countdown wording.
func TestDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want string
		span time.Duration
	}{
		{span: -time.Hour, want: "less than a minute"},
		{span: 0, want: "less than a minute"},
		{span: 59 * time.Second, want: "less than a minute"},
		{span: time.Minute, want: "1 min"},
		{span: 90 * time.Second, want: "2 min"},
		{span: 59 * time.Minute, want: "59 min"},
		{span: 59*time.Minute + 45*time.Second, want: "1 hr"},
		{span: time.Hour, want: "1 hr"},
		{span: 72 * time.Minute, want: "1 hr 12 min"},
		{span: 23*time.Hour + 59*time.Minute, want: "23 hr 59 min"},
		{span: 24 * time.Hour, want: "1 day 0 hr"},
		{span: 25*time.Hour + 30*time.Minute, want: "1 day 1 hr"},
		{span: 47*time.Hour + 59*time.Minute, want: "1 day 23 hr"},
		{span: 48 * time.Hour, want: "2 days"},
		{span: 6*24*time.Hour + 23*time.Hour, want: "6 days"},
	}

	for _, tt := range tests {
		t.Run(tt.span.String(), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Duration(tt.span))
		})
	}
}

// TestEscape protects the markup characters and leaves the rest alone.
func TestEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want string
	}{
		{text: "", want: ""},
		{text: "plain text", want: "plain text"},
		{text: "<b>bold</b>", want: "&lt;b&gt;bold&lt;/b&gt;"},
		{text: "Q&A", want: "Q&amp;A"},
		{text: "&lt;", want: "&amp;lt;"},
		{text: `"quoted" 'text'`, want: `"quoted" 'text'`},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Escape(tt.text))
		})
	}
}

// TestClock omits the weekday for a reset later today.
func TestClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		moment time.Time
		name   string
		want   string
	}{
		{name: "today", moment: messageNow.Add(5*time.Hour + 20*time.Minute), want: "3:20 PM"},
		{name: "tomorrow", moment: messageNow.Add(24 * time.Hour), want: "Thu 10:00 AM"},
		{name: "same weekday next week", moment: messageNow.AddDate(0, 0, 7), want: "Wed 10:00 AM"},
		{name: "same day next year", moment: messageNow.AddDate(1, 0, 0), want: "Thu 10:00 AM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, clock(tt.moment, messageNow))
		})
	}
}
