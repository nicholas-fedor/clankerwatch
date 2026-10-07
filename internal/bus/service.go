// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// Logger records recoverable events.
type Logger interface {
	// Warn records a recoverable event.
	//
	// Parameters:
	//   - ctx: cancellation and request scope.
	//   - msg: static message.
	//   - args: key-value pairs.
	Warn(ctx context.Context, msg string, args ...any)
}

// PropertySetter stores a property value and emits PropertiesChanged.
// A [prop.Properties] satisfies it.
type PropertySetter interface {
	// SetMust sets a property and panics on an invalid name or a failed emit.
	//
	// Parameters:
	//   - iface: the interface name.
	//   - property: the property name.
	//   - value: the new value.
	SetMust(iface, property string, value any)
}

// Exports holds the object's initial property values and the handlers its
// methods call.
type Exports struct {
	// OnRefresh is called for every Refresh call, from a bus goroutine.
	OnRefresh func()

	// OnSetSettings is called for every SetSettings call, from a bus
	// goroutine. It receives the settings JSON and returns an error message,
	// or an empty string on success.
	OnSetSettings func(settings string) string

	// Snapshot is the initial snapshot JSON.
	Snapshot string

	// Settings is the initial settings JSON.
	Settings string

	// Version is the daemon version.
	Version string
}

// Service is the exported object.
type Service struct {
	props PropertySetter
	log   Logger
	last  map[string]string
	mu    sync.Mutex
}

// Bus coordinates of the service. The plasmoid and the activation files use
// the same names, which the policy-check task verifies.
const (
	// BusName is the well-known name on the session bus.
	BusName = "com.nickfedor.ClankerWatch1"

	// Interface is the object's interface.
	Interface = BusName

	// ObjectPath is the object's path.
	ObjectPath = dbus.ObjectPath("/com/nickfedor/ClankerWatch1")

	// introspectable is the standard introspection interface.
	introspectable = "org.freedesktop.DBus.Introspectable"

	// propertySnapshot is the snapshot property.
	propertySnapshot = "Snapshot"

	// propertySettings is the settings property.
	propertySettings = "Settings"

	// propertyVersion is the version property.
	propertyVersion = "Version"

	// methodRefresh is the refresh method.
	methodRefresh = "Refresh"

	// methodSetSettings is the method that changes settings.
	methodSetSettings = "SetSettings"

	// logKeyErr is the log key for an error.
	logKeyErr = "err"

	// logKeyProperty is the log key for a property name.
	logKeyProperty = "property"
)

// ErrAlreadyRunning indicates that another process owns the bus name.
var ErrAlreadyRunning = errors.New("another clankerwatch instance owns the bus name")

// parents lists the nodes above ObjectPath, so `busctl tree` can walk to it.
var parents = []struct{ path, child string }{
	{"/", "com"},
	{"/com", "nickfedor"},
	{"/com/nickfedor", "ClankerWatch1"},
}

// Serve exports the object and then claims the bus name.
//
// Parameters:
//   - conn: the session bus connection.
//   - exports: the initial property values and the method handlers.
//   - log: receives publishing failures.
//
// Returns:
//   - *Service: the exported object.
//   - error: ErrAlreadyRunning when another process owns the name, or an
//     export error.
func Serve(conn *dbus.Conn, exports Exports, log Logger) (*Service, error) {
	methods := map[string]any{
		methodRefresh: func() *dbus.Error {
			exports.OnRefresh()

			return nil
		},
		methodSetSettings: func(settings string) (string, *dbus.Error) {
			return exports.OnSetSettings(settings), nil
		},
	}

	err := conn.ExportMethodTable(methods, ObjectPath, Interface)
	if err != nil {
		return nil, fmt.Errorf("export methods: %w", err)
	}

	props, err := prop.Export(conn, ObjectPath, prop.Map{Interface: {
		propertySnapshot: {Value: exports.Snapshot, Writable: false, Emit: prop.EmitTrue, Callback: nil},
		propertySettings: {Value: exports.Settings, Writable: false, Emit: prop.EmitTrue, Callback: nil},
		propertyVersion:  {Value: exports.Version, Writable: false, Emit: prop.EmitConst, Callback: nil},
	}})
	if err != nil {
		return nil, fmt.Errorf("export properties: %w", err)
	}

	err = exportIntrospection(conn, props)
	if err != nil {
		return nil, err
	}

	reply, err := conn.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request bus name: %w", err)
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, ErrAlreadyRunning
	}

	return NewService(props, exports, log), nil
}

// NewService returns a service that publishes through props.
//
// Parameters:
//   - props: the exported properties.
//   - exports: the values props already holds.
//   - log: receives publishing failures.
//
// Returns:
//   - *Service: the service.
func NewService(props PropertySetter, exports Exports, log Logger) *Service {
	return &Service{
		props: props,
		log:   log,
		last:  map[string]string{propertySnapshot: exports.Snapshot, propertySettings: exports.Settings},
		mu:    sync.Mutex{},
	}
}

// Publish sets a new snapshot.
//
// An unchanged snapshot emits nothing. A failed emit is logged instead of
// crashing the daemon, and the snapshot is offered again on the next change.
//
// Parameters:
//   - snapshot: the snapshot JSON.
func (s *Service) Publish(snapshot string) {
	s.set(propertySnapshot, snapshot)
}

// PublishSettings sets new settings, the same way Publish sets a snapshot.
//
// Parameters:
//   - settings: the settings JSON.
func (s *Service) PublishSettings(settings string) {
	s.set(propertySettings, settings)
}

// set stores a property value and emits it when it changed.
//
// Parameters:
//   - property: the property name.
//   - value: the JSON value.
func (s *Service) set(property, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if value == s.last[property] {
		return
	}

	defer func() {
		recovered := recover()
		if recovered != nil {
			s.log.Warn(context.Background(), "Publishing a property failed",
				logKeyProperty, property,
				logKeyErr, recovered)
		}
	}()

	s.props.SetMust(Interface, property, value)

	s.last[property] = value
}

// exportIntrospection exports introspection for the object and its parents.
//
// Parameters:
//   - conn: the session bus connection.
//   - props: the exported properties, which describe themselves.
//
// Returns:
//   - error: an export error.
func exportIntrospection(conn *dbus.Conn, props *prop.Properties) error {
	node := &introspect.Node{
		XMLName: introspect.Node{}.XMLName,
		Name:    string(ObjectPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:        Interface,
				Methods:     methodIntrospection(),
				Signals:     nil,
				Properties:  props.Introspection(Interface),
				Annotations: nil,
			},
		},
		Children: nil,
	}

	err := conn.Export(introspect.NewIntrospectable(node), ObjectPath, introspectable)
	if err != nil {
		return fmt.Errorf("export introspection: %w", err)
	}

	for _, parent := range parents {
		child := introspect.Node{XMLName: node.XMLName, Name: parent.child, Interfaces: nil, Children: nil}
		parentNode := &introspect.Node{
			XMLName: node.XMLName, Name: parent.path, Interfaces: nil, Children: []introspect.Node{child},
		}

		err = conn.Export(introspect.NewIntrospectable(parentNode), dbus.ObjectPath(parent.path), introspectable)
		if err != nil {
			return fmt.Errorf("export introspection: %w", err)
		}
	}

	return nil
}

// methodIntrospection describes the object's methods.
//
// Returns:
//   - [][introspect.Method]: Refresh and SetSettings with their arguments.
func methodIntrospection() []introspect.Method {
	return []introspect.Method{
		{Name: methodRefresh, Args: nil, Annotations: nil},
		{
			Name: methodSetSettings,
			Args: []introspect.Arg{
				{Name: "settings", Type: "s", Direction: "in"},
				{Name: "error", Type: "s", Direction: "out"},
			},
			Annotations: nil,
		},
	}
}
