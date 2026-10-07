// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/bus"
	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/daemon/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/engine"
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

	// notificationsName is the notification server's bus name.
	notificationsName = "org.freedesktop.Notifications"

	// eventWait bounds how long a test waits for the daemon.
	eventWait = 10 * time.Second
)

// errConnect is returned by the failing connector.
var errConnect = errors.New("no session bus")

// recordingLogger keeps every message and announces each one.
type recordingLogger struct {
	events   chan string
	messages []string
	mu       sync.Mutex
}

// fakeNotifications is a notification server that records summaries.
type fakeNotifications struct {
	summaries chan string
}

// TestNew checks the runner keeps the logger and version and connects to the
// session bus.
func TestNew(t *testing.T) {
	t.Parallel()

	log := mocks.NewMockLogger(t)
	runner := New(log, "v1.2.3")

	assert.Same(t, log, runner.log)
	assert.Equal(t, "v1.2.3", runner.version)
	assert.NotNil(t, runner.connect)
}

// TestConnectSessionUsesTheEnvironment checks the session address is read
// from DBUS_SESSION_BUS_ADDRESS.
//
// The test points it at a private bus, so the user's session is never
// touched.
func TestConnectSessionUsesTheEnvironment(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", startBus(t))

	conn, err := connectSession()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.True(t, conn.Connected())
}

// TestConnectSessionReportsAFailure checks an unreachable bus is wrapped.
func TestConnectSessionReportsAFailure(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "missing"))

	conn, err := connectSession()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect to the session bus")
	assert.Nil(t, conn)
}

// TestLocalDeps checks only hybrid mode wires the login and the network.
//
// Cache-only promises never to read credentials or call the endpoint, so the
// collaborators that could do either must be absent.
func TestLocalDeps(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cfg := testConfig(t, home)
	paths := claudecode.ResolvePaths("", home)

	hybrid := localDeps(cfg, paths, newRecordingLogger(), "v1")
	assert.NotNil(t, hybrid.Credentials)
	assert.NotNil(t, hybrid.Fetcher)
	assert.NotNil(t, hybrid.Global)
	assert.NotNil(t, hybrid.Activity)
	assert.NotNil(t, hybrid.Store)
	assert.Nil(t, hybrid.Notifier)

	cfg.Mode = config.ModeCacheOnly

	cacheOnly := localDeps(cfg, paths, newRecordingLogger(), "v1")
	assert.Nil(t, cacheOnly.Credentials)
	assert.Nil(t, cacheOnly.Fetcher)
	assert.NotNil(t, cacheOnly.Global)
	assert.Nil(t, cacheOnly.Notifier)
}

// TestRunnerReportsAConnectError checks every bus operation stops on a failed
// connection.
func TestRunnerReportsAConnectError(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		log:     newRecordingLogger(),
		connect: func() (*dbus.Conn, error) { return nil, errConnect },
		version: "v1",
	}
	cfg := testConfig(t, t.TempDir())

	require.ErrorIs(t, runner.Serve(t.Context(), cfg, nil), errConnect)
	require.ErrorIs(t, runner.Demo(t.Context(), cfg), errConnect)
	require.ErrorIs(t, runner.NotifyTest(t.Context()), errConnect)
}

// TestServeReportsATakenName checks a second instance fails to export.
func TestServeReportsATakenName(t *testing.T) {
	t.Parallel()

	address := startBus(t)

	owner := connect(t, address)
	reply, err := owner.RequestName(bus.BusName, dbus.NameFlagDoNotQueue)
	require.NoError(t, err)
	require.Equal(t, dbus.RequestNameReplyPrimaryOwner, reply)

	runner := &Runner{log: newRecordingLogger(), connect: dialer(address), version: "v1"}

	err = runner.Serve(t.Context(), testConfig(t, t.TempDir()), nil)
	require.ErrorIs(t, err, bus.ErrAlreadyRunning)
	assert.Contains(t, err.Error(), "export the service")
}

// TestServeStopsWhenTheBusDrops checks a lost connection ends the daemon.
//
// systemd restarts the unit, so returning cleanly is better than serving an
// object nobody can reach.
func TestServeStopsWhenTheBusDrops(t *testing.T) {
	t.Parallel()

	address := startBus(t)
	log := newRecordingLogger()

	var (
		conn *dbus.Conn
		mu   sync.Mutex
	)

	runner := &Runner{
		log: log,
		connect: func() (*dbus.Conn, error) {
			opened, err := dbus.Connect(address)
			if err != nil {
				return nil, err
			}

			mu.Lock()
			conn = opened
			mu.Unlock()

			return opened, nil
		},
		version: "v1",
	}

	cfg := testConfig(t, t.TempDir())
	done := make(chan error, 1)

	go func() { done <- runner.Serve(t.Context(), cfg, nil) }()

	log.waitFor(t, "Started")

	mu.Lock()
	require.NoError(t, conn.Close())
	mu.Unlock()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(eventWait):
		require.FailNow(t, "Serve did not return after the connection closed")
	}

	assert.True(t, log.has("Lost the session bus connection"))
	assert.True(t, log.has("Stopped"))
}

// TestNotifyTestSendsOneNotification checks the sample reaches the server.
func TestNotifyTestSendsOneNotification(t *testing.T) {
	t.Parallel()

	address := startBus(t)
	server := serveNotifications(t, connect(t, address))
	runner := &Runner{log: newRecordingLogger(), connect: dialer(address), version: "v1"}

	require.NoError(t, runner.NotifyTest(t.Context()))

	select {
	case summary := <-server.summaries:
		assert.Equal(t, "Clanker Watch test notification", summary)
	case <-time.After(eventWait):
		require.FailNow(t, "the notification did not arrive")
	}

	assert.Empty(t, server.summaries, "exactly one notification")
}

// TestNotifyTestReportsAMissingServer checks the failure is wrapped.
func TestNotifyTestReportsAMissingServer(t *testing.T) {
	t.Parallel()

	runner := &Runner{log: newRecordingLogger(), connect: dialer(startBus(t)), version: "v1"}

	err := runner.NotifyTest(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test notification")
}

// TestRunUntilChangeReturnsOnAChange checks a SetSettings change ends the run
// with the store's settings and a stopped engine.
func TestRunUntilChangeReturnsOnAChange(t *testing.T) {
	t.Parallel()

	runner, eng, cfg := newEngineRunner(t)
	cfg.Interval = 7 * time.Minute
	store := newSettingsStore(cfg, nil)
	changed := make(chan struct{}, 1)
	changed <- struct{}{}

	next, ok := runner.runUntilChange(t.Context(), eng, store, changed, make(chan os.Signal))

	assert.True(t, ok)
	assert.Equal(t, cfg, next)
	assert.FileExists(t, cfg.StateFile(), "the engine saved its state before returning")
}

// TestRunUntilChangeReturnsOnAHangup checks SIGHUP reloads the settings and
// ends the run with the result.
//
// The test feeds the hangup channel directly, so no signal reaches the test
// process.
func TestRunUntilChangeReturnsOnAHangup(t *testing.T) {
	t.Parallel()

	runner, eng, cfg := newEngineRunner(t)
	reloaded := cfg
	reloaded.Mode = config.ModeHybrid
	reloaded.Interval = 9 * time.Minute

	store := newSettingsStore(cfg, func() (config.Config, error) { return reloaded, nil })
	hangups := make(chan os.Signal, 1)
	hangups <- syscall.SIGHUP

	next, ok := runner.runUntilChange(t.Context(), eng, store, make(chan struct{}), hangups)

	assert.True(t, ok)
	assert.Equal(t, reloaded, next)
	assert.Equal(t, reloaded, store.Current())
	assert.FileExists(t, cfg.StateFile())
}

// TestRunUntilChangeSurvivesAFailedReload checks a hangup whose reload fails
// is logged and the engine keeps running with the old settings.
//
// A typo in a hand-edited file must not stop the daemon. The next hangup,
// after the file is fixed, still applies.
func TestRunUntilChangeSurvivesAFailedReload(t *testing.T) {
	t.Parallel()

	runner, eng, cfg := newEngineRunner(t)
	log, ok := runner.log.(*recordingLogger)
	require.True(t, ok)

	fixed := cfg
	fixed.Interval = 11 * time.Minute

	var calls atomic.Int64

	store := newSettingsStore(cfg, func() (config.Config, error) {
		if calls.Add(1) == 1 {
			return config.Config{}, errConnect
		}

		return fixed, nil
	})

	hangups := make(chan os.Signal)
	done := make(chan config.Config, 1)

	go func() {
		next, changed := runner.runUntilChange(t.Context(), eng, store, make(chan struct{}), hangups)
		assert.True(t, changed)
		done <- next
	}()

	hangups <- syscall.SIGHUP

	log.waitFor(t, "Reloading the settings failed")

	select {
	case <-done:
		require.FailNow(t, "runUntilChange returned after a failed reload")
	case <-time.After(100 * time.Millisecond):
	}

	assert.Equal(t, cfg, store.Current())

	hangups <- syscall.SIGHUP

	select {
	case next := <-done:
		assert.Equal(t, fixed, next)
	case <-time.After(eventWait):
		require.FailNow(t, "runUntilChange did not return after a good reload")
	}

	assert.Equal(t, int64(2), calls.Load())
}

// TestRunUntilChangeStopsWithTheContext checks a canceled context ends the
// run without new settings.
func TestRunUntilChangeStopsWithTheContext(t *testing.T) {
	t.Parallel()

	runner, eng, cfg := newEngineRunner(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	next, ok := runner.runUntilChange(ctx, eng, newSettingsStore(cfg, nil), make(chan struct{}), make(chan os.Signal))

	assert.False(t, ok)
	assert.Equal(t, config.Config{}, next)
	assert.FileExists(t, cfg.StateFile())
}

// TestSetSettings checks the widget's change is applied, signaled once, and
// answered with an empty message.
//
// The changed channel holds one value, so a second change before the run
// loop reads it must not block the D-Bus goroutine.
func TestSetSettings(t *testing.T) {
	t.Parallel()

	log := newRecordingLogger()
	runner := &Runner{log: log, connect: nil, version: "v1"}
	store, _, path := newFileStore(t, nil)
	changed := make(chan struct{}, 1)

	assert.Empty(t, runner.setSettings(t.Context(), store, changed, `{"mode":"cache-only"}`))
	assert.Empty(t, runner.setSettings(t.Context(), store, changed, `{"intervalSeconds":600}`))

	assert.Len(t, changed, 1)
	assert.Equal(t, config.ModeCacheOnly, store.Current().Mode)
	assert.Equal(t, 10*time.Minute, store.Current().Interval)
	assert.Contains(t, readFile(t, path), "mode: cache-only")
	assert.False(t, log.has("Rejected a settings change"))
}

// TestSetSettingsRejects checks a failed change is logged, answered with the
// error message, and not signaled.
func TestSetSettingsRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want  error
		name  string
		patch string
	}{
		{name: "invalid", patch: `{"bogus":true}`, want: ErrInvalidPatch},
		{name: "unusable", patch: `{"intervalSeconds":1}`, want: config.ErrIntervalTooShort},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := newRecordingLogger()
			runner := &Runner{log: log, connect: nil, version: "v1"}
			store, _, path := newFileStore(t, nil)
			changed := make(chan struct{}, 1)

			_, wantErr := newSettingsStore(store.Current(), store.reload).Apply(tt.patch)
			require.ErrorIs(t, wantErr, tt.want)

			message := runner.setSettings(t.Context(), store, changed, tt.patch)

			assert.Equal(t, wantErr.Error(), message)
			assert.Empty(t, changed)
			assert.True(t, log.has("Rejected a settings change"))
			assert.NoFileExists(t, path)
		})
	}

	t.Run("not editable", func(t *testing.T) {
		t.Parallel()

		runner := &Runner{log: newRecordingLogger(), connect: nil, version: "v1"}
		store := newSettingsStore(testConfig(t, t.TempDir()), nil)

		assert.Equal(t, ErrNotEditable.Error(), runner.setSettings(t.Context(), store, make(chan struct{}, 1), "{}"))
	})
}

// Notify records the summary and returns a fixed id.
func (f *fakeNotifications) Notify(
	_ string, _ uint32, _, summary, _ string, _ []string, _ map[string]dbus.Variant, _ int32,
) (uint32, *dbus.Error) {
	f.summaries <- summary

	return 7, nil
}

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

// testConfig returns valid settings rooted in home with alerts on.
func testConfig(t *testing.T, home string) config.Config {
	t.Helper()

	cfg := config.Default(config.Dirs{Home: home, StateHome: filepath.Join(home, "state")})
	require.NoError(t, cfg.Validate())

	return cfg
}

// newEngineRunner returns a runner with a recording logger, a cache-only
// engine over a temporary home, and its settings.
//
// Cache-only never reads credentials or uses the network.
func newEngineRunner(t *testing.T) (*Runner, *engine.Engine, config.Config) {
	t.Helper()

	cfg := testConfig(t, t.TempDir())
	cfg.Mode = config.ModeCacheOnly
	cfg.Thresholds = nil
	cfg.NotifyAuth = false

	log := newRecordingLogger()
	runner := &Runner{log: log, connect: nil, version: "v1"}
	deps := localDeps(cfg, claudecode.ResolvePaths(cfg.ClaudeDir, cfg.Home), log, runner.version)

	return runner, engine.New(t.Context(), engine.OptionsFrom(cfg), deps), cfg
}

// serveNotifications exports a fake notification server on conn.
func serveNotifications(t *testing.T, conn *dbus.Conn) *fakeNotifications {
	t.Helper()

	server := &fakeNotifications{summaries: make(chan string, 4)}

	err := conn.Export(server, "/org/freedesktop/Notifications", notificationsName)
	require.NoError(t, err)

	reply, err := conn.RequestName(notificationsName, dbus.NameFlagDoNotQueue)
	require.NoError(t, err)
	require.Equal(t, dbus.RequestNameReplyPrimaryOwner, reply)

	return server
}

// dialer returns a connector for address.
func dialer(address string) func() (*dbus.Conn, error) {
	return func() (*dbus.Conn, error) { return dbus.Connect(address) }
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
