// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/bus"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/daemon"
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

	// eventWait bounds how long a test waits for the daemon.
	eventWait = 15 * time.Second

	// pollEvery is how often a test checks for the bus name.
	pollEvery = 20 * time.Millisecond

	// accountUUID is the account in the cached usage fixture.
	accountUUID = "00000000-0000-4000-8000-000000000001"
)

// nopLogger discards every event.
type nopLogger struct{}

// recordingLogger keeps every message and announces each one.
type recordingLogger struct {
	events   chan string
	messages []string
	mu       sync.Mutex
}

// settingsView is the part of the Settings property the tests read.
type settingsView struct {
	File            string    `json:"file"`
	Mode            string    `json:"mode"`
	Notify          []float64 `json:"notify"`
	IntervalSeconds int64     `json:"intervalSeconds"`
	Editable        bool      `json:"editable"`
}

// snapshotView is the part of a snapshot the tests read.
type snapshotView struct {
	Status string            `json:"status"`
	Mode   string            `json:"mode"`
	Bars   []json.RawMessage `json:"bars"`
}

// TestStatus checks the offline snapshot for each mode and fixture.
//
// No fixture holds credentials, so hybrid mode can only report a missing
// login and the engine never reaches the network.
func TestStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mode   config.Mode
		want   string
		bars   bool
		cached bool
	}{
		{name: "cache-only with Claude Code's cache", mode: config.ModeCacheOnly, cached: true, want: "ok", bars: true},
		{name: "cache-only without a cache", mode: config.ModeCacheOnly, want: "no_data"},
		{name: "hybrid without credentials", mode: config.ModeHybrid, want: "logged_out"},
		{name: "hybrid without credentials but cached", mode: config.ModeHybrid, cached: true, want: "logged_out", bars: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if tt.cached {
				writeCachedUsage(t, home)
			}

			cfg := testConfig(t, home)
			cfg.Mode = tt.mode

			report := daemon.New(nopLogger{}, "v1.2.3").Status(t.Context(), cfg)

			assert.Equal(t, "v1.2.3", report.Version)
			assert.Equal(t, tt.mode, report.Mode)
			assert.Equal(t, filepath.Join(home, ".claude", ".credentials.json"), report.Credentials)
			assert.Equal(t, filepath.Join(home, ".claude.json"), report.GlobalConfig)
			assert.Equal(t, filepath.Join(home, ".claude", "sessions"), report.Sessions)
			assert.Equal(t, cfg.StateFile(), report.StateFile)

			snap := decodeSnapshot(t, string(report.Snapshot))
			assert.Equal(t, tt.want, snap.Status)
			assert.Equal(t, string(tt.mode), snap.Mode)
			assert.Equal(t, tt.bars, len(snap.Bars) > 0, "bars")

			assert.NoFileExists(t, cfg.StateFile(), "status must not write state")
		})
	}
}

// TestServe checks the daemon claims its name and stops on cancellation.
//
// DBUS_SESSION_BUS_ADDRESS points at a private bus and the home is a
// temporary directory, so neither the user's session nor ~/.claude is used.
func TestServe(t *testing.T) {
	address := isolate(t)
	home := os.Getenv("HOME")
	client := connect(t, address)

	reload := fileReload(t, home, filepath.Join(home, "config", "clankerwatch", "config.yaml"))
	cfg, err := reload()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- daemon.New(nopLogger{}, "v1.2.3").Serve(ctx, cfg, reload) }()

	waitForName(t, client)

	var version string

	err = client.Object(bus.BusName, bus.ObjectPath).
		Call(propertiesInterface+".Get", 0, bus.Interface, "Version").Store(&version)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", version)

	var snapshot string

	err = client.Object(bus.BusName, bus.ObjectPath).
		Call(propertiesInterface+".Get", 0, bus.Interface, "Snapshot").Store(&snapshot)
	require.NoError(t, err)
	assert.Equal(t, "logged_out", decodeSnapshot(t, snapshot).Status)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(eventWait):
		require.FailNow(t, "Serve did not stop after cancellation")
	}

	assert.FileExists(t, filepath.Join(home, "state", "clankerwatch", "state.json"), "state is saved on shutdown")
}

// TestDemo checks the demo publishes changing snapshots and refuses
// settings changes.
//
// Every PropertiesChanged carries a snapshot that differs from the one
// before, so two signals prove the scripted data moves. The demo has no
// settings file to write, so SetSettings must report ErrNotEditable.
func TestDemo(t *testing.T) {
	address := isolate(t)
	client := connect(t, address)
	signals := subscribe(t, client)

	cfg := testConfig(t, os.Getenv("HOME"))
	cfg.Thresholds = nil
	cfg.NotifyAuth = false

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- daemon.New(nopLogger{}, "v1.2.3").Demo(ctx, cfg) }()

	first := nextSnapshot(t, signals)
	second := nextSnapshot(t, signals)

	assert.NotEqual(t, first, second)

	view := decodeSnapshot(t, first)
	assert.Equal(t, "ok", view.Status)
	assert.NotEmpty(t, view.Bars)

	settings := decodeSettings(t, getProperty(t, client, "Settings"))
	assert.False(t, settings.Editable, "the demo cannot change settings")
	assert.Equal(t, daemon.ErrNotEditable.Error(), setSettings(t, client, `{"mode":"cache-only"}`))

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(eventWait):
		require.FailNow(t, "Demo did not stop after cancellation")
	}
}

// TestServeAppliesSettings checks the widget's settings flow end to end.
//
// A valid SetSettings call writes the settings file, publishes the new
// Settings, and restarts the engine. An unusable or malformed change is
// answered with its error and leaves the file and the published settings
// alone. The bus is private and the home is temporary.
func TestServeAppliesSettings(t *testing.T) {
	address := isolate(t)
	home := os.Getenv("HOME")
	client := connect(t, address)
	signals := subscribe(t, client)

	path := filepath.Join(home, "config", "clankerwatch", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("mode: cache-only\nnotify: []\nnotifyAuth: false\n"), 0o600))

	reload := fileReload(t, home, path)
	cfg, err := reload()
	require.NoError(t, err)

	log := newRecordingLogger()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- daemon.New(log, "v1.2.3").Serve(ctx, cfg, reload) }()

	waitForName(t, client)

	initial := decodeSettings(t, getProperty(t, client, "Settings"))
	assert.True(t, initial.Editable)
	assert.Equal(t, path, initial.File)
	assert.Equal(t, "cache-only", initial.Mode)
	assert.Equal(t, int64(300), initial.IntervalSeconds)

	assert.Empty(t, setSettings(t, client, `{"intervalSeconds":600,"notify":[90]}`))

	changed := decodeSettings(t, nextChange(t, signals, "Settings"))
	assert.Equal(t, int64(600), changed.IntervalSeconds)
	assert.Equal(t, []float64{90}, changed.Notify)
	assert.Equal(t, "cache-only", changed.Mode)

	log.waitFor(t, "Applied new settings")

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "interval: 10m")

	for _, patch := range []string{`{"intervalSeconds":60}`, `{"bogus":1}`, `{"mode":"bogus"}`} {
		reply := setSettings(t, client, patch)
		assert.NotEmpty(t, reply, patch)

		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, string(written), string(after), "a rejected change leaves the file for %s", patch)
	}

	assert.Contains(t, setSettings(t, client, `{"intervalSeconds":60}`), "interval is below the minimum")
	assert.True(t, log.has("Rejected a settings change"))
	assert.Equal(t, changed, decodeSettings(t, getProperty(t, client, "Settings")))

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(eventWait):
		require.FailNow(t, "Serve did not stop after cancellation")
	}

	assert.True(t, log.has("Stopped"))
}

// Debug discards the event.
func (nopLogger) Debug(context.Context, string, ...any) {}

// Info discards the event.
func (nopLogger) Info(context.Context, string, ...any) {}

// Warn discards the event.
func (nopLogger) Warn(context.Context, string, ...any) {}

// Debug records msg.
func (l *recordingLogger) Debug(_ context.Context, msg string, _ ...any) { l.record(msg) }

// Info records msg.
func (l *recordingLogger) Info(_ context.Context, msg string, _ ...any) { l.record(msg) }

// Warn records msg.
func (l *recordingLogger) Warn(_ context.Context, msg string, _ ...any) { l.record(msg) }

// record stores msg and announces it without blocking.
func (l *recordingLogger) record(msg string) {
	l.mu.Lock()
	l.messages = append(l.messages, msg)
	l.mu.Unlock()

	select {
	case l.events <- msg:
	default:
	}
}

// has reports whether msg was logged.
func (l *recordingLogger) has(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Contains(l.messages, msg)
}

// waitFor blocks until msg is logged.
func (l *recordingLogger) waitFor(t *testing.T, msg string) {
	t.Helper()

	deadline := time.After(eventWait)

	for !l.has(msg) {
		select {
		case <-l.events:
		case <-deadline:
			require.FailNow(t, "the daemon did not log "+msg)
		}
	}
}

// newRecordingLogger returns an empty recording logger.
func newRecordingLogger() *recordingLogger {
	return &recordingLogger{events: make(chan string, 64), messages: nil, mu: sync.Mutex{}}
}

// isolate points the session bus and every user directory at test-owned
// locations and returns the private bus address.
func isolate(t *testing.T) string {
	t.Helper()

	address := startBus(t)
	home := t.TempDir()

	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))

	return address
}

// testConfig returns valid settings rooted in home.
func testConfig(t *testing.T, home string) config.Config {
	t.Helper()

	cfg := config.Default(config.Dirs{Home: home, StateHome: filepath.Join(home, "state")})
	require.NoError(t, cfg.Validate())

	return cfg
}

// writeCachedUsage writes a global config whose usage cache Claude Code
// fetched a minute ago for the signed-in account.
func writeCachedUsage(t *testing.T, home string) {
	t.Helper()

	now := time.Now()
	content, err := json.Marshal(map[string]any{
		"oauthAccount": map[string]any{"accountUuid": accountUUID},
		"cachedUsageUtilization": map[string]any{
			"accountUuid": accountUUID,
			"fetchedAtMs": now.Add(-time.Minute).UnixMilli(),
			"utilization": map[string]any{
				"five_hour": map[string]any{"utilization": 42, "resets_at": now.Add(time.Hour).UTC().Format(time.RFC3339)},
				"seven_day": map[string]any{"utilization": 7, "resets_at": now.Add(72 * time.Hour).UTC().Format(time.RFC3339)},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), content, 0o600))
}

// decodeSnapshot parses the fields of a snapshot the tests read.
func decodeSnapshot(t *testing.T, snapshot string) snapshotView {
	t.Helper()

	var view snapshotView

	require.NoError(t, json.Unmarshal([]byte(snapshot), &view), snapshot)

	return view
}

// fileReload returns a reload function that resolves the settings file at
// path over the defaults for home, the way the serve command does without
// flags or environment.
func fileReload(t *testing.T, home, path string) daemon.Reload {
	t.Helper()

	return func() (config.Config, error) {
		cfg := testConfig(t, home)
		cfg.SettingsFile = path

		file, err := config.LoadOptionalFile(path)
		if err != nil {
			return config.Config{}, err
		}

		err = file.Apply(&cfg)
		if err != nil {
			return config.Config{}, err
		}

		err = cfg.Validate()
		if err != nil {
			return config.Config{}, err
		}

		return cfg, nil
	}
}

// decodeSettings parses the fields of the Settings property the tests read.
func decodeSettings(t *testing.T, settings string) settingsView {
	t.Helper()

	var view settingsView

	require.NoError(t, json.Unmarshal([]byte(settings), &view), settings)

	return view
}

// getProperty reads one string property from the daemon.
func getProperty(t *testing.T, client *dbus.Conn, property string) string {
	t.Helper()

	var value string

	err := client.Object(bus.BusName, bus.ObjectPath).
		Call(propertiesInterface+".Get", 0, bus.Interface, property).Store(&value)
	require.NoError(t, err)

	return value
}

// setSettings calls SetSettings and returns its error message.
func setSettings(t *testing.T, client *dbus.Conn, patch string) string {
	t.Helper()

	var reply string

	err := client.Object(bus.BusName, bus.ObjectPath).
		Call(bus.Interface+".SetSettings", 0, patch).Store(&reply)
	require.NoError(t, err)

	return reply
}

// subscribe delivers the object's PropertiesChanged signals to a channel.
func subscribe(t *testing.T, client *dbus.Conn) <-chan *dbus.Signal {
	t.Helper()

	err := client.AddMatchSignal(
		dbus.WithMatchObjectPath(bus.ObjectPath),
		dbus.WithMatchInterface(propertiesInterface),
		dbus.WithMatchMember("PropertiesChanged"),
	)
	require.NoError(t, err)

	signals := make(chan *dbus.Signal, 64)
	client.Signal(signals)

	return signals
}

// waitForName blocks until the daemon owns its bus name.
func waitForName(t *testing.T, client *dbus.Conn) {
	t.Helper()

	assert.Eventually(t, func() bool {
		var owned bool

		err := client.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, bus.BusName).Store(&owned)

		return err == nil && owned
	}, eventWait, pollEvery)
}

// nextSnapshot waits for the next PropertiesChanged that carries a snapshot
// and returns it.
func nextSnapshot(t *testing.T, signals <-chan *dbus.Signal) string {
	t.Helper()

	return nextChange(t, signals, "Snapshot")
}

// nextChange waits for the next PropertiesChanged that carries property and
// returns its value. Signals for other properties are skipped.
func nextChange(t *testing.T, signals <-chan *dbus.Signal, property string) string {
	t.Helper()

	timeout := time.After(eventWait)

	for {
		select {
		case signal := <-signals:
			if signal.Name != propertiesInterface+".PropertiesChanged" || len(signal.Body) < 2 {
				continue
			}

			changed, ok := signal.Body[1].(map[string]dbus.Variant)
			require.True(t, ok)

			variant, present := changed[property]
			if !present {
				continue
			}

			value, ok := variant.Value().(string)
			require.True(t, ok)

			return value
		case <-timeout:
			require.FailNow(t, "no PropertiesChanged signal for "+property+" arrived")
		}
	}
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
