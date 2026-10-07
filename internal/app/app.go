// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/nicholas-fedor/clankerwatch/internal/bus"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/demo"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/notifytest"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/serve"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/status"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/daemon"
	"github.com/nicholas-fedor/clankerwatch/internal/engine"
	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/version"
)

const (
	// exitOK is a successful command.
	exitOK = 0

	// exitFailure is a runtime failure.
	exitFailure = 1

	// exitInvalidSetting is a flag or environment value that cannot be used.
	exitInvalidSetting = 2
)

var (
	_ serve.Server      = (*daemon.Runner)(nil)
	_ demo.Demoer       = (*daemon.Runner)(nil)
	_ status.Reporter   = (*daemon.Runner)(nil)
	_ notifytest.Sender = (*daemon.Runner)(nil)
	_ daemon.Logger     = (*logger)(nil)
	_ engine.Logger     = (*logger)(nil)
	_ bus.Logger        = (*logger)(nil)
	_ cmd.LevelSetter   = (*logger)(nil)
	_ engine.Notifier   = (*notify.DBusSender)(nil)
)

// Run executes one clankerwatch invocation.
//
// Parameters:
//   - ctx: cancellation, including signal shutdown.
//
// Returns:
//   - int: the process status.
func Run(ctx context.Context) int {
	return execute(ctx, os.Stdout, os.Stderr, os.Args[1:], os.LookupEnv)
}

// execute runs the command tree with explicit streams and arguments.
//
// Parameters:
//   - ctx: cancellation.
//   - stdout: command output.
//   - stderr: diagnostics and errors.
//   - args: command-line arguments without the program name.
//   - lookupEnv: reads environment variables.
//
// Returns:
//   - int: the process status.
func execute(ctx context.Context, stdout, stderr io.Writer, args []string, lookupEnv options.LookupEnv) int {
	log := newLogger(stderr, lookupEnv)
	info := version.Current()

	root := cmd.NewRoot(ctx, cmd.Dependencies{
		Serve:      daemon.New(log, info.Version),
		Demo:       daemon.New(log, info.Version),
		Status:     daemon.New(log, info.Version),
		NotifyTest: daemon.New(log, info.Version),
		Levels:     log,
		Stdout:     stdout,
		LookupEnv:  lookupEnv,
		Dirs:       config.SystemDirs(),
		Version:    versionLine(info),
	})
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}

	err = errors.Join(err, options.WriteString(stderr, options.Name+": "+err.Error()+"\n"))

	return codeFor(err)
}

// codeFor maps a command error to a process status.
//
// Parameters:
//   - err: error returned by the command tree.
//
// Returns:
//   - int: 2 for an invalid setting, otherwise 1.
func codeFor(err error) int {
	if errors.Is(err, options.ErrInvalidSetting) {
		return exitInvalidSetting
	}

	return exitFailure
}

// versionLine renders the version command's output.
//
// Parameters:
//   - info: the build metadata.
//
// Returns:
//   - string: such as "v0.1.0 (abc1234, 2026-10-06T12:00:00Z)".
func versionLine(info version.Info) string {
	return info.Version + " (" + info.CommitSHA + ", " + info.BuildTime + ")"
}
