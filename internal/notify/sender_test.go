// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/notify/mocks"
)

// testApp is the application name the sender tests use.
const testApp = "clankerwatch"

// errNoServer is the call failure the sender tests inject.
var errNoServer = errors.New("org.freedesktop.DBus.Error.ServiceUnknown")

// sampleMessage returns a critical notification that replaces another.
//
// Returns:
//   - Message: the notification.
func sampleMessage() Message {
	return Message{
		Summary:    "Current session at 96%",
		Body:       "Resets in 1 hr (11:00 AM)",
		Icon:       "dialog-error",
		Urgency:    2,
		ExpireMs:   0,
		ReplacesID: 5,
	}
}

// notifyArgs returns the Notify arguments the sender must pass for message.
//
// Parameters:
//   - message: the notification.
//
// Returns:
//   - []any: the method arguments in the specification's order.
func notifyArgs(message Message) []any {
	return []any{
		testApp,
		message.ReplacesID,
		message.Icon,
		message.Summary,
		message.Body,
		[]string{},
		map[string]dbus.Variant{"urgency": dbus.MakeVariant(message.Urgency)},
		message.ExpireMs,
	}
}

// boundedContext matches a context with a deadline within the send timeout.
//
// Returns:
//   - any: the argument matcher.
func boundedContext() any {
	return mock.MatchedBy(func(ctx context.Context) bool {
		deadline, ok := ctx.Deadline()

		return ok && time.Until(deadline) <= sendTimeout
	})
}

// TestNewSender keeps the caller and the application name.
func TestNewSender(t *testing.T) {
	t.Parallel()

	caller := mocks.NewMockCaller(t)
	sender := NewSender(caller, testApp)

	assert.Same(t, caller, sender.caller)
	assert.Equal(t, testApp, sender.app)
}

// TestNewDBusSender targets the notification server object on the bus.
func TestNewDBusSender(t *testing.T) {
	t.Parallel()

	sender := NewDBusSender(&dbus.Conn{}, testApp)

	object, ok := sender.caller.(*dbus.Object)
	require.True(t, ok)
	assert.Equal(t, "org.freedesktop.Notifications", object.Destination())
	assert.Equal(t, dbus.ObjectPath("/org/freedesktop/Notifications"), object.Path())
	assert.Equal(t, testApp, sender.app)
}

// TestSendPassesTheMessage calls Notify with every field in order and returns
// the server's ID.
func TestSendPassesTheMessage(t *testing.T) {
	t.Parallel()

	message := sampleMessage()
	caller := mocks.NewMockCaller(t)
	caller.EXPECT().
		CallWithContext(boundedContext(), "org.freedesktop.Notifications.Notify", dbus.Flags(0), notifyArgs(message)).
		Return(&dbus.Call{Body: []any{uint32(7)}}).
		Once()

	id, err := NewSender(caller, testApp).Send(t.Context(), message)
	require.NoError(t, err)
	assert.Equal(t, uint32(7), id)
}

// TestSendFailures reports call and decode failures with a zero ID.
func TestSendFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		call    *dbus.Call
		name    string
	}{
		{name: "call error", call: &dbus.Call{Err: errNoServer}, wantErr: errNoServer},
		{name: "wrong reply type", call: &dbus.Call{Body: []any{"seven"}}},
		{name: "empty reply", call: &dbus.Call{Body: []any{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			caller := mocks.NewMockCaller(t)
			caller.EXPECT().
				CallWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(tt.call).
				Once()

			id, err := NewSender(caller, testApp).Send(t.Context(), sampleMessage())
			require.Error(t, err)
			assert.Zero(t, id)
			assert.Contains(t, err.Error(), "send notification")

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

// TestSendHonorsCancellation passes the caller's cancellation through.
func TestSendHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	caller := mocks.NewMockCaller(t)
	caller.EXPECT().
		CallWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, _ dbus.Flags, _ ...any) *dbus.Call {
			return &dbus.Call{Err: ctx.Err()}
		}).
		Once()

	_, err := NewSender(caller, testApp).Send(ctx, sampleMessage())
	require.ErrorIs(t, err, context.Canceled)
}
