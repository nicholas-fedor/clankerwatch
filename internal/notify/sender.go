// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// Caller invokes a D-Bus method. A [dbus.BusObject] satisfies it.
type Caller interface {
	// CallWithContext calls method with args and waits for the reply.
	//
	// Parameters:
	//   - ctx: cancellation and deadline for the call.
	//   - method: the fully qualified method name.
	//   - flags: call flags.
	//   - args: the method arguments.
	//
	// Returns:
	//   - *[dbus.Call]: the completed call.
	CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...any) *dbus.Call
}

// DBusSender posts notifications to the desktop's notification server.
type DBusSender struct {
	caller Caller
	app    string
}

const (
	// notificationsService is the notification server's bus name.
	notificationsService = "org.freedesktop.Notifications"

	// notificationsPath is the notification server's object path.
	notificationsPath = dbus.ObjectPath("/org/freedesktop/Notifications")

	// notifyMethod posts a notification.
	notifyMethod = notificationsService + ".Notify"

	// sendTimeout bounds one notification call.
	sendTimeout = 5 * time.Second
)

// NewDBusSender returns a sender for the notification server on conn.
//
// Parameters:
//   - conn: the session bus connection.
//   - app: the application name shown with notifications.
//
// Returns:
//   - *DBusSender: the sender.
func NewDBusSender(conn *dbus.Conn, app string) *DBusSender {
	return NewSender(conn.Object(notificationsService, notificationsPath), app)
}

// NewSender returns a sender that posts through caller.
//
// Parameters:
//   - caller: the notification server object.
//   - app: the application name shown with notifications.
//
// Returns:
//   - *DBusSender: the sender.
func NewSender(caller Caller, app string) *DBusSender {
	return &DBusSender{caller: caller, app: app}
}

// Send posts message.
//
// Parameters:
//   - ctx: cancellation for the call, which is also bounded to five seconds.
//   - message: the notification.
//
// Returns:
//   - uint32: the notification ID the server assigned.
//   - error: the call or decode error.
func (s *DBusSender) Send(ctx context.Context, message Message) (uint32, error) {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	hints := map[string]dbus.Variant{"urgency": dbus.MakeVariant(message.Urgency)}
	call := s.caller.CallWithContext(ctx, notifyMethod, 0,
		s.app, message.ReplacesID, message.Icon, message.Summary, message.Body, []string{}, hints, message.ExpireMs)

	var id uint32

	err := call.Store(&id)
	if err != nil {
		return 0, fmt.Errorf("send notification: %w", err)
	}

	return id, nil
}
