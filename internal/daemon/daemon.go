// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/nicholas-fedor/clankerwatch/internal/bus"
	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/engine"
	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// Logger records diagnostic events for the daemon and the packages it wires.
type Logger interface {
	// Debug records a debug event.
	//
	// Parameters:
	//   - ctx: cancellation and request scope.
	//   - msg: static message.
	//   - args: key-value pairs.
	Debug(ctx context.Context, msg string, args ...any)

	// Info records an informational event.
	//
	// Parameters:
	//   - ctx: cancellation and request scope.
	//   - msg: static message.
	//   - args: key-value pairs.
	Info(ctx context.Context, msg string, args ...any)

	// Warn records a recoverable event.
	//
	// Parameters:
	//   - ctx: cancellation and request scope.
	//   - msg: static message.
	//   - args: key-value pairs.
	Warn(ctx context.Context, msg string, args ...any)
}

// Reload resolves the settings again from the settings file, the environment,
// and the flags, the same way the daemon started.
type Reload = func() (config.Config, error)

// prepareFunc adjusts the engine's options and collaborators before it starts.
type prepareFunc func(opt engine.Options, deps engine.Deps) (engine.Options, engine.Deps)

// Runner runs the daemon's operations.
type Runner struct {
	log     Logger
	connect func() (*dbus.Conn, error)
	version string
}

const (
	// appName is the application name shown with notifications.
	appName = "Clanker Watch"

	// activityWindow is how recently a session file must have changed to
	// count as activity.
	activityWindow = 10 * time.Minute

	// testNotifyTimeout bounds the test notification.
	testNotifyTimeout = 10 * time.Second
)

// Log keys.
const (
	logKeyVersion = "version"
	logKeyMode    = "mode"
	logKeyErr     = "err"
)

// New returns a runner that connects to the session bus.
//
// Parameters:
//   - log: the logger.
//   - version: the daemon version.
//
// Returns:
//   - *Runner: the runner.
func New(log Logger, version string) *Runner {
	return &Runner{log: log, connect: connectSession, version: version}
}

// Demo serves canned states for widget development.
//
// Parameters:
//   - ctx: cancellation, normally from a signal.
//   - cfg: the settings, whose thresholds are kept.
//
// Returns:
//   - error: a connection or export error.
func (r *Runner) Demo(ctx context.Context, cfg config.Config) error {
	return r.run(ctx, cfg, nil, func(opt engine.Options, deps engine.Deps) (engine.Options, engine.Deps) {
		return engine.Demo(opt, deps.Notifier, r.log)
	})
}

// NotifyTest sends one sample notification.
//
// Parameters:
//   - ctx: cancellation for the call.
//
// Returns:
//   - error: a connection or send error.
func (r *Runner) NotifyTest(ctx context.Context) error {
	conn, err := r.connect()
	if err != nil {
		return err
	}

	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(ctx, testNotifyTimeout)
	defer cancel()

	_, err = notify.NewDBusSender(conn, appName).Send(ctx, notify.Message{
		Summary:    "Clanker Watch test notification",
		Body:       "Usage alerts will look like this.",
		Icon:       "dialog-information",
		Urgency:    1,
		ExpireMs:   -1,
		ReplacesID: 0,
	})
	if err != nil {
		return fmt.Errorf("test notification: %w", err)
	}

	return nil
}

// Serve runs the daemon until ctx is canceled or the bus connection drops.
//
// The widget's SetSettings calls and SIGHUP apply new settings without a
// restart.
//
// Parameters:
//   - ctx: cancellation, normally from a signal.
//   - cfg: the settings.
//   - reload: resolves the settings again after the settings file changes.
//
// Returns:
//   - error: a connection or export error.
func (r *Runner) Serve(ctx context.Context, cfg config.Config, reload Reload) error {
	return r.run(ctx, cfg, reload, func(opt engine.Options, deps engine.Deps) (engine.Options, engine.Deps) {
		return opt, deps
	})
}

// Status reports the settings and the snapshot the daemon would publish now.
//
// It reads local files only and never uses the network.
//
// Parameters:
//   - ctx: scope for the reads.
//   - cfg: the settings.
//
// Returns:
//   - Report: the settings, paths, and snapshot.
func (r *Runner) Status(ctx context.Context, cfg config.Config) Report {
	paths := claudecode.ResolvePaths(cfg.ClaudeDir, cfg.Home)

	deps := localDeps(cfg, paths, r.log, r.version)

	deps.Fetcher = nil

	eng := engine.New(ctx, engine.OptionsFrom(cfg), deps)

	return newReport(r.version, cfg, paths, eng.Snapshot())
}

// newEngine builds an engine for the settings.
//
// Parameters:
//   - ctx: scope for restoring state.
//   - conn: the session bus connection, for notifications.
//   - cfg: the settings.
//   - prep: adjusts the engine's options and collaborators.
//
// Returns:
//   - *[engine.Engine]: the engine, not yet running.
func (r *Runner) newEngine(ctx context.Context, conn *dbus.Conn, cfg config.Config, prep prepareFunc) *engine.Engine {
	opt := engine.OptionsFrom(cfg)
	deps := localDeps(cfg, claudecode.ResolvePaths(cfg.ClaudeDir, cfg.Home), r.log, r.version)

	if len(cfg.Thresholds) > 0 || cfg.NotifyAuth {
		deps.Notifier = notify.NewDBusSender(conn, appName)
	}

	opt, deps = prep(opt, deps)

	return engine.New(ctx, opt, deps)
}

// run connects, builds the engine, exports it, and drives it.
//
// A settings change stops the engine, which saves its state, and starts a
// new one with the new settings, which restores it.
//
// Parameters:
//   - ctx: cancellation.
//   - cfg: the settings.
//   - reload: resolves the settings again, or nil when they cannot change.
//   - prepare: adjusts the engine's options and collaborators before it starts.
//
// Returns:
//   - error: a connection or export error.
func (r *Runner) run(ctx context.Context, cfg config.Config, reload Reload, prepare prepareFunc) error {
	conn, err := r.connect()
	if err != nil {
		return err
	}

	defer func() { _ = conn.Close() }()

	store := newSettingsStore(cfg, reload)
	changed := make(chan struct{}, 1)
	eng := r.newEngine(ctx, conn, cfg, prepare)

	var current atomic.Pointer[engine.Engine]

	current.Store(eng)

	service, err := bus.Serve(conn, bus.Exports{
		OnRefresh:     func() { current.Load().RequestRefresh() },
		OnSetSettings: func(patch string) string { return r.setSettings(ctx, store, changed, patch) },
		Snapshot:      eng.Snapshot(),
		Settings:      store.View(),
		Version:       r.version,
	}, r.log)
	if err != nil {
		return fmt.Errorf("export the service: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go r.watchConnection(runCtx, conn, cancel)

	hangups := make(chan os.Signal, 1)
	signal.Notify(hangups, syscall.SIGHUP)

	defer signal.Stop(hangups)

	r.log.Info(ctx, "Started",
		logKeyVersion, r.version,
		logKeyMode, cfg.Mode)

	for {
		eng.SetPublisher(service.Publish)
		service.Publish(eng.Snapshot())       //nolint:contextcheck // Publishing logs without a request scope.
		service.PublishSettings(store.View()) //nolint:contextcheck // Publishing logs without a request scope.

		next, ok := r.runUntilChange(runCtx, eng, store, changed, hangups)
		if !ok {
			break
		}

		eng = r.newEngine(ctx, conn, next, prepare)
		current.Store(eng)

		r.log.Info(ctx, "Applied new settings", logKeyMode, next.Mode)
	}

	r.log.Info(ctx, "Stopped")

	return nil
}

// runUntilChange runs an engine until ctx ends or the settings change.
//
// The engine has stopped and saved its state when this returns.
//
// Parameters:
//   - ctx: cancellation.
//   - eng: the engine.
//   - store: the settings store.
//   - changed: receives a value after the widget changes the settings.
//   - hangups: receives SIGHUP, which reloads the settings file.
//
// Returns:
//   - config.Config: the new settings.
//   - bool: false when ctx ended.
func (r *Runner) runUntilChange(
	ctx context.Context,
	eng *engine.Engine,
	store *settingsStore,
	changed <-chan struct{},
	hangups <-chan os.Signal,
) (config.Config, bool) {
	// The engine runs in its own goroutine so this loop can watch for changes.
	engineCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)

		eng.Run(engineCtx)
	}()

	defer func() {
		stop()
		<-done
	}()

	for {
		select {
		case <-ctx.Done():
			return config.Config{}, false
		case <-changed:
			return store.Current(), true
		case <-hangups:
			cfg, err := store.Reload()
			if err != nil {
				r.log.Warn(ctx, "Reloading the settings failed", logKeyErr, err)

				continue
			}

			return cfg, true
		}
	}
}

// setSettings applies a settings change from the widget.
//
// Parameters:
//   - ctx: scope for logs.
//   - store: the settings store.
//   - changed: told about a successful change.
//   - patch: the change as JSON.
//
// Returns:
//   - string: the error message, or empty on success.
func (r *Runner) setSettings(ctx context.Context, store *settingsStore, changed chan<- struct{}, patch string) string {
	_, err := store.Apply(patch)
	if err != nil {
		r.log.Warn(ctx, "Rejected a settings change", logKeyErr, err)

		return err.Error()
	}

	select {
	case changed <- struct{}{}:
	default:
	}

	return ""
}

// watchConnection cancels the daemon when the bus connection drops.
//
// Parameters:
//   - ctx: ends the watch.
//   - conn: the session bus connection.
//   - cancel: stops the daemon.
func (r *Runner) watchConnection(ctx context.Context, conn *dbus.Conn, cancel context.CancelFunc) {
	select {
	case <-conn.Context().Done():
		r.log.Warn(ctx, "Lost the session bus connection")
		cancel()
	case <-ctx.Done():
	}
}

// connectSession connects to the session bus.
//
// Returns:
//   - *[dbus.Conn]: the connection.
//   - error: the connection error.
func connectSession() (*dbus.Conn, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect to the session bus: %w", err)
	}

	return conn, nil
}

// localDeps wires the readers for Claude Code's files and, in hybrid mode,
// the usage endpoint client.
//
// Parameters:
//   - cfg: the settings.
//   - paths: Claude Code's file locations.
//   - log: the logger.
//   - version: the daemon version for the User-Agent.
//
// Returns:
//   - engine.Deps: the collaborators, without a notifier.
func localDeps(cfg config.Config, paths claudecode.Paths, log Logger, version string) engine.Deps {
	deps := engine.Deps{
		Credentials: nil,
		Global:      claudecode.NewGlobalConfigReader(paths.GlobalConfig),
		Activity:    claudecode.NewActivityProbe(paths.Sessions, activityWindow),
		Fetcher:     nil,
		Notifier:    nil,
		Store:       state.File{Path: cfg.StateFile()},
		Logger:      log,
	}

	if cfg.Mode == config.ModeHybrid {
		deps.Credentials = claudecode.NewCredentialsReader(paths.Credentials)
		deps.Fetcher = usage.NewClient(version)
	}

	return deps
}
