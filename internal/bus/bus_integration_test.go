// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package bus_test

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	introspection "github.com/godbus/dbus/v5/introspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/bus"
	"github.com/nicholas-fedor/clankerwatch/internal/bus/mocks"
)

const (
	// busConfig is a minimal session bus without service activation, so no
	// test can start a real daemon or notification server by name.
	busConfig = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:dir=%s</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`

	// propertiesInterface is the standard properties interface.
	propertiesInterface = "org.freedesktop.DBus.Properties"

	// propertiesChanged is the full name of the change signal.
	propertiesChanged = propertiesInterface + ".PropertiesChanged"

	// signalWait bounds how long a test waits for a signal.
	signalWait = 5 * time.Second

	// quietWait is how long a test listens to prove no signal follows.
	quietWait = 300 * time.Millisecond
)

// TestServeIntrospection checks introspection describes the object and the
// nodes above it.
//
// busctl tree and the plasmoid's D-Bus browser walk down from "/", so every
// parent node must list its child. The plasmoid calls SetSettings with one
// string and reads one string back, so the method must declare both.
func TestServeIntrospection(t *testing.T) {
	t.Parallel()

	server, client := connectPair(t)

	_, err := bus.Serve(server, exports("{}", "{}", nil), mocks.NewMockLogger(t))
	require.NoError(t, err)

	object := introspect(t, client, bus.ObjectPath)
	assert.Contains(t, object, `name="Snapshot"`)
	assert.Contains(t, object, `name="Settings"`)
	assert.Contains(t, object, `name="Version"`)
	assert.Contains(t, object, `name="Refresh"`)
	assert.Contains(t, object, `name="SetSettings"`)
	assert.Contains(t, object, `name="`+bus.Interface+`"`)

	var node introspection.Node

	require.NoError(t, xml.Unmarshal([]byte(object), &node))

	var setSettings *introspection.Method

	for _, iface := range node.Interfaces {
		if iface.Name != bus.Interface {
			continue
		}

		for i := range iface.Methods {
			if iface.Methods[i].Name == "SetSettings" {
				setSettings = &iface.Methods[i]
			}
		}
	}

	require.NotNil(t, setSettings, "SetSettings is introspected")
	assert.Equal(t, []introspection.Arg{
		{Name: "settings", Type: "s", Direction: "in"},
		{Name: "error", Type: "s", Direction: "out"},
	}, setSettings.Args)

	parents := map[dbus.ObjectPath]string{
		"/":              "com",
		"/com":           "nickfedor",
		"/com/nickfedor": "ClankerWatch1",
	}
	for path, child := range parents {
		assert.Contains(t, introspect(t, client, path), `<node name="`+child+`">`, "children of %s", path)
	}
}

// TestServeGetAll checks every property is readable with its value.
func TestServeGetAll(t *testing.T) {
	t.Parallel()

	server, client := connectPair(t)

	_, err := bus.Serve(server, exports(`{"v":1}`, `{"v":2}`, nil), mocks.NewMockLogger(t))
	require.NoError(t, err)

	var values map[string]dbus.Variant

	err = client.Object(bus.BusName, bus.ObjectPath).
		Call(propertiesInterface+".GetAll", 0, bus.Interface).Store(&values)
	require.NoError(t, err)

	require.Contains(t, values, "Snapshot")
	require.Contains(t, values, "Settings")
	require.Contains(t, values, "Version")
	assert.Equal(t, `{"v":1}`, values["Snapshot"].Value())
	assert.Equal(t, `{"v":2}`, values["Settings"].Value())
	assert.Equal(t, "1.2.3", values["Version"].Value())
}

// TestPublishEmitsOnePropertiesChanged checks the signal count per snapshot.
//
// A new snapshot emits exactly one PropertiesChanged carrying the value. An
// identical snapshot emits nothing, which is proven by the next signal
// carrying the following snapshot.
func TestPublishEmitsOnePropertiesChanged(t *testing.T) {
	t.Parallel()

	server, client := connectPair(t)

	service, err := bus.Serve(server, exports("initial", "{}", nil), mocks.NewMockLogger(t))
	require.NoError(t, err)

	signals := subscribe(t, client)

	service.Publish("first")
	assert.Equal(t, "first", nextChange(t, signals, "Snapshot"))

	service.Publish("first")
	service.Publish("second")
	assert.Equal(t, "second", nextChange(t, signals, "Snapshot"))

	service.Publish("second")
	assertQuiet(t, signals)

	assert.Equal(t, "second", getProperty(t, client, "Snapshot"))
}

// TestPublishSettingsEmitsOnePropertiesChanged checks the signal count per
// settings value.
//
// The widget's settings page reloads on every Settings signal, so a repeat
// must emit nothing, and a snapshot publish must not carry Settings.
func TestPublishSettingsEmitsOnePropertiesChanged(t *testing.T) {
	t.Parallel()

	server, client := connectPair(t)

	service, err := bus.Serve(server, exports("{}", "initial settings", nil), mocks.NewMockLogger(t))
	require.NoError(t, err)

	signals := subscribe(t, client)

	service.PublishSettings("initial settings")
	service.PublishSettings("hybrid settings")
	assert.Equal(t, "hybrid settings", nextChange(t, signals, "Settings"))

	service.PublishSettings("hybrid settings")
	service.Publish("hybrid settings")
	assert.Equal(t, "hybrid settings", nextChange(t, signals, "Snapshot"))

	service.PublishSettings("cache-only settings")
	assert.Equal(t, "cache-only settings", nextChange(t, signals, "Settings"))

	service.PublishSettings("cache-only settings")
	assertQuiet(t, signals)

	assert.Equal(t, "cache-only settings", getProperty(t, client, "Settings"))
}

// TestRefreshReachesTheCallback checks the Refresh method calls OnRefresh.
func TestRefreshReachesTheCallback(t *testing.T) {
	t.Parallel()

	server, client := connectPair(t)
	refreshed := make(chan struct{}, 1)

	served := exports("{}", "{}", nil)
	served.OnRefresh = func() { refreshed <- struct{}{} }

	_, err := bus.Serve(server, served, mocks.NewMockLogger(t))
	require.NoError(t, err)

	call := client.Object(bus.BusName, bus.ObjectPath).Call(bus.Interface+".Refresh", 0)
	require.NoError(t, call.Err)

	select {
	case <-refreshed:
	case <-time.After(signalWait):
		t.Fatal("Refresh did not reach the callback")
	}
}

// TestSetSettingsRoundTrips checks SetSettings passes its argument to
// OnSetSettings and returns the handler's message unchanged.
//
// An empty reply means success to the plasmoid, and anything else is shown
// as the error, so both must survive the bus.
func TestSetSettingsRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		argument string
		reply    string
	}{
		{name: "success", argument: `{"mode":"cache-only"}`, reply: ""},
		{name: "failure", argument: `{"bogus":1}`, reply: "invalid settings change: unknown field"},
		{name: "empty argument", argument: "", reply: "invalid settings change: EOF"},
		{name: "unicode", argument: `{"mode":"\u00e9"}`, reply: "invalid mode \"é\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, client := connectPair(t)
			received := make(chan string, 1)

			_, err := bus.Serve(server, exports("{}", "{}", func(settings string) string {
				received <- settings

				return tt.reply
			}), mocks.NewMockLogger(t))
			require.NoError(t, err)

			var reply string

			err = client.Object(bus.BusName, bus.ObjectPath).
				Call(bus.Interface+".SetSettings", 0, tt.argument).Store(&reply)
			require.NoError(t, err)
			assert.Equal(t, tt.reply, reply)

			select {
			case argument := <-received:
				assert.Equal(t, tt.argument, argument)
			case <-time.After(signalWait):
				require.FailNow(t, "SetSettings did not reach the handler")
			}
		})
	}
}

// TestServeTwiceReturnsErrAlreadyRunning checks a second instance gives up.
//
// D-Bus activation may race a manual start, so the loser must report the
// sentinel instead of serving a second copy.
func TestServeTwiceReturnsErrAlreadyRunning(t *testing.T) {
	t.Parallel()

	first, second := connectPair(t)

	_, err := bus.Serve(first, exports("{}", "{}", nil), mocks.NewMockLogger(t))
	require.NoError(t, err)

	service, err := bus.Serve(second, exports("{}", "{}", nil), mocks.NewMockLogger(t))
	require.ErrorIs(t, err, bus.ErrAlreadyRunning)
	assert.Nil(t, service)
}

// TestServeReportsAClosedConnection checks a failed name request is wrapped
// rather than reported as another running instance.
func TestServeReportsAClosedConnection(t *testing.T) {
	t.Parallel()

	conn, err := dbus.Connect(startBus(t))
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	service, err := bus.Serve(conn, exports("{}", "{}", nil), mocks.NewMockLogger(t))
	require.Error(t, err)
	require.NotErrorIs(t, err, bus.ErrAlreadyRunning)
	assert.Contains(t, err.Error(), "request bus name")
	assert.Nil(t, service)
}

// exports returns exports with the initial values, a no-op OnRefresh, and
// onSetSettings, or a handler that accepts everything when it is nil.
func exports(snapshot, settings string, onSetSettings func(string) string) bus.Exports {
	if onSetSettings == nil {
		onSetSettings = func(string) string { return "" }
	}

	return bus.Exports{
		OnRefresh:     func() {},
		OnSetSettings: onSetSettings,
		Snapshot:      snapshot,
		Settings:      settings,
		Version:       "1.2.3",
	}
}

// getProperty reads one string property from the service.
func getProperty(t *testing.T, conn *dbus.Conn, property string) string {
	t.Helper()

	var value string

	err := conn.Object(bus.BusName, bus.ObjectPath).
		Call(propertiesInterface+".Get", 0, bus.Interface, property).Store(&value)
	require.NoError(t, err)

	return value
}

// startBus runs a private session bus for one test and returns its address.
//
// The daemon is killed when the test ends. The test is skipped when
// dbus-daemon is not installed.
func startBus(t *testing.T) string {
	t.Helper()

	daemonPath, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not on PATH")
	}

	// The socket path must fit in sun_path, so it lives in a short directory
	// rather than under t.TempDir.
	dir, err := os.MkdirTemp("", "cwbus") //nolint:usetesting // t.TempDir paths can exceed the socket path limit.
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	configPath := filepath.Join(dir, "bus.conf")
	err = os.WriteFile(configPath, fmt.Appendf(nil, busConfig, dir), 0o600)
	require.NoError(t, err)

	command := exec.CommandContext(t.Context(), daemonPath, "--config-file="+configPath, "--nofork", "--print-address=1")

	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	address, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)

	return strings.TrimSpace(address)
}

// connect opens a connection to address that is closed when the test ends.
func connect(t *testing.T, address string) *dbus.Conn {
	t.Helper()

	conn, err := dbus.Connect(address)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

// connectPair starts a private bus and opens two connections to it.
func connectPair(t *testing.T) (*dbus.Conn, *dbus.Conn) {
	t.Helper()

	address := startBus(t)

	return connect(t, address), connect(t, address)
}

// introspect returns the introspection XML of path on the service.
func introspect(t *testing.T, conn *dbus.Conn, path dbus.ObjectPath) string {
	t.Helper()

	var data string

	err := conn.Object(bus.BusName, path).Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&data)
	require.NoError(t, err)

	return data
}

// subscribe delivers the object's PropertiesChanged signals to a channel.
func subscribe(t *testing.T, conn *dbus.Conn) <-chan *dbus.Signal {
	t.Helper()

	err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(bus.ObjectPath),
		dbus.WithMatchInterface(propertiesInterface),
		dbus.WithMatchMember("PropertiesChanged"),
	)
	require.NoError(t, err)

	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)

	return signals
}

// nextChange waits for the next PropertiesChanged and returns the value of
// property, which the signal must carry alone.
func nextChange(t *testing.T, signals <-chan *dbus.Signal, property string) string {
	t.Helper()

	timeout := time.After(signalWait)

	for {
		select {
		case signal := <-signals:
			if signal.Name != propertiesChanged {
				continue
			}

			require.Len(t, signal.Body, 3)
			assert.Equal(t, bus.Interface, signal.Body[0])

			changed, ok := signal.Body[1].(map[string]dbus.Variant)
			require.True(t, ok, "changed properties have type %T", signal.Body[1])
			require.Len(t, changed, 1, "one property per signal")
			require.Contains(t, changed, property)

			value, ok := changed[property].Value().(string)
			require.True(t, ok)

			return value
		case <-timeout:
			require.FailNow(t, "no PropertiesChanged signal arrived")
		}
	}
}

// assertQuiet checks no PropertiesChanged arrives for a short while.
func assertQuiet(t *testing.T, signals <-chan *dbus.Signal) {
	t.Helper()

	timeout := time.After(quietWait)

	for {
		select {
		case signal := <-signals:
			assert.NotEqual(t, propertiesChanged, signal.Name, "unexpected signal %v", signal.Body)
		case <-timeout:
			return
		}
	}
}
