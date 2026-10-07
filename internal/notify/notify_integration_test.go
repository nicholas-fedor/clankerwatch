// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package notify_test exercises threshold alerts through public APIs, from
// evaluation to a mocked notification server and back into the ledger.
package notify_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/notify/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// flowNow is the start of the alert flows.
var flowNow = time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

// flowThresholds are the warning and critical thresholds.
var flowThresholds = []float64{80, 95}

// notifyReply captures the arguments of one Notify call and replies with id.
//
// Parameters:
//   - captured: receives the method arguments.
//   - id: the notification ID the server assigns.
//
// Returns:
//   - func: the mock implementation.
func notifyReply(captured *[][]any, id uint32) func(context.Context, string, dbus.Flags, ...any) *dbus.Call {
	return func(_ context.Context, _ string, _ dbus.Flags, args ...any) *dbus.Call {
		*captured = append(*captured, args)

		return &dbus.Call{Body: []any{id}}
	}
}

// deliver evaluates bars, sends every due alert, and records it with the
// server's ID, the way the daemon does.
//
// Parameters:
//   - t: test handle.
//   - ledger: the ledger.
//   - sender: the notification sender.
//   - now: the current time.
//   - bars: the current bars.
//
// Returns:
//   - []notify.Message: the messages sent.
func deliver(t *testing.T, ledger *notify.Ledger, sender *notify.DBusSender, now time.Time, bars ...usage.Bar) []notify.Message {
	t.Helper()

	alerts := ledger.Evaluate(now, bars, flowThresholds)
	sent := make([]notify.Message, 0, len(alerts))

	for _, alert := range alerts {
		message := notify.Format(alert, now)
		message.ReplacesID = ledger.ReplaceIDs[alert.Bar.ID]

		id, err := sender.Send(t.Context(), message)
		require.NoError(t, err)

		ledger.Record(alert, id, now)

		sent = append(sent, message)
	}

	return sent
}

// TestAlertFlow climbs through both thresholds, replaces the warning with the
// critical alert, never repeats, and survives a JSON round trip of the ledger.
func TestAlertFlow(t *testing.T) {
	t.Parallel()

	var captured [][]any

	caller := mocks.NewMockCaller(t)
	caller.EXPECT().
		CallWithContext(mock.Anything, "org.freedesktop.Notifications.Notify", dbus.Flags(0), mock.Anything).
		RunAndReturn(notifyReply(&captured, 21)).
		Once()
	caller.EXPECT().
		CallWithContext(mock.Anything, "org.freedesktop.Notifications.Notify", dbus.Flags(0), mock.Anything).
		RunAndReturn(notifyReply(&captured, 22)).
		Once()

	sender := notify.NewSender(caller, "clankerwatch")
	resets := flowNow.Add(3 * time.Hour)
	bar := func(percent float64) usage.Bar {
		return usage.Bar{ID: "session", Label: "Current session", Percent: percent, ResetsAt: resets}
	}

	var ledger notify.Ledger

	assert.Empty(t, deliver(t, &ledger, sender, flowNow, bar(60)))

	sent := deliver(t, &ledger, sender, flowNow.Add(time.Minute), bar(82))
	require.Len(t, sent, 1)
	assert.Equal(t, "Current session at 82%", sent[0].Summary)
	assert.Equal(t, byte(1), sent[0].Urgency)
	assert.Zero(t, sent[0].ReplacesID)

	assert.Empty(t, deliver(t, &ledger, sender, flowNow.Add(2*time.Minute), bar(90)))

	// The daemon restarts and restores the ledger from its state file.
	encoded, err := json.Marshal(ledger)
	require.NoError(t, err)

	var restored notify.Ledger

	require.NoError(t, json.Unmarshal(encoded, &restored))

	sent = deliver(t, &restored, sender, flowNow.Add(3*time.Minute), bar(96))
	require.Len(t, sent, 1)
	assert.Equal(t, byte(2), sent[0].Urgency)
	assert.Equal(t, uint32(21), sent[0].ReplacesID, "the critical alert replaces the warning")

	assert.Empty(t, deliver(t, &restored, sender, flowNow.Add(4*time.Minute), bar(100)))

	require.Len(t, captured, 2)
	assert.Equal(t, "clankerwatch", captured[0][0])
	assert.Equal(t, uint32(0), captured[0][1])
	assert.Equal(t, uint32(21), captured[1][1])
	assert.Equal(t, "Current session at 96%", captured[1][3])
	assert.Equal(t, uint32(22), restored.ReplaceIDs["session"])
}

// TestAlertFlowJump sends a single critical alert for a jump past both
// thresholds and nothing for a credit bar.
func TestAlertFlowJump(t *testing.T) {
	t.Parallel()

	var captured [][]any

	caller := mocks.NewMockCaller(t)
	caller.EXPECT().
		CallWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(notifyReply(&captured, 9)).
		Once()

	sender := notify.NewSender(caller, "clankerwatch")
	session := usage.Bar{ID: "session", Label: "Current session", Percent: 50, ResetsAt: flowNow.Add(time.Hour)}
	credit := usage.Bar{ID: "credit", Label: "Extra usage", Percent: 100, Credit: true}

	var ledger notify.Ledger

	assert.Empty(t, deliver(t, &ledger, sender, flowNow, session, credit))

	session.Percent = 97

	sent := deliver(t, &ledger, sender, flowNow.Add(time.Minute), session, credit)
	require.Len(t, sent, 1)
	assert.Equal(t, "dialog-error", sent[0].Icon)
	assert.Equal(t, int32(0), sent[0].ExpireMs)
	assert.Len(t, ledger.Entries, 2)

	session.ResetsAt = session.ResetsAt.Add(4 * time.Minute)
	assert.Empty(t, deliver(t, &ledger, sender, flowNow.Add(2*time.Minute), session, credit))
	assert.Len(t, captured, 1)
}

// TestAlertFlowSendFailure leaves the ledger untouched when the server is
// unavailable, so the alert is retried on the next poll.
func TestAlertFlowSendFailure(t *testing.T) {
	t.Parallel()

	caller := mocks.NewMockCaller(t)
	caller.EXPECT().
		CallWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&dbus.Call{Err: dbus.ErrClosed}).
		Once()
	caller.EXPECT().
		CallWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&dbus.Call{Body: []any{uint32(3)}}).
		Once()

	sender := notify.NewSender(caller, "clankerwatch")
	bar := usage.Bar{ID: "session", Label: "Current session", Percent: 85, ResetsAt: flowNow.Add(time.Hour)}

	var ledger notify.Ledger

	alerts := ledger.Evaluate(flowNow, []usage.Bar{bar}, flowThresholds)
	require.Len(t, alerts, 1)

	_, err := sender.Send(t.Context(), notify.Format(alerts[0], flowNow))
	require.ErrorIs(t, err, dbus.ErrClosed)

	alerts = ledger.Evaluate(flowNow.Add(time.Minute), []usage.Bar{bar}, flowThresholds)
	require.Len(t, alerts, 1)

	id, err := sender.Send(t.Context(), notify.Format(alerts[0], flowNow))
	require.NoError(t, err)

	ledger.Record(alerts[0], id, flowNow.Add(time.Minute))
	assert.Empty(t, ledger.Evaluate(flowNow.Add(2*time.Minute), []usage.Bar{bar}, flowThresholds))
}

// TestAuthMessageFlow sends the login notice through the sender.
func TestAuthMessageFlow(t *testing.T) {
	t.Parallel()

	var captured [][]any

	caller := mocks.NewMockCaller(t)
	caller.EXPECT().
		CallWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(notifyReply(&captured, 4)).
		Once()

	id, err := notify.NewSender(caller, "clankerwatch").Send(t.Context(), notify.AuthMessage())
	require.NoError(t, err)
	assert.Equal(t, uint32(4), id)

	require.Len(t, captured, 1)
	assert.Equal(t, "dialog-password", captured[0][2])
	assert.Equal(t, "Claude Code login needs attention", captured[0][3])
}
