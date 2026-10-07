// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"io"
	"log/slog"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/demo"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/notifytest"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/serve"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/status"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// LevelSetter receives the resolved log level.
//
// The composition root wires its logger before cobra parses anything, so the
// level is reported back through this hook.
type LevelSetter interface {
	// SetLevel sets the minimum log level.
	//
	// Parameters:
	//   - level: the minimum level.
	SetLevel(level slog.Level)
}

// Dependencies are the process inputs the command tree needs.
//
// The service fields are contracts declared by the command packages that
// consume them, so a command depends only on what it uses.
type Dependencies struct {
	// Serve runs the daemon.
	Serve serve.Server

	// Demo replays canned states.
	Demo demo.Demoer

	// Status reports the offline snapshot.
	Status status.Reporter

	// NotifyTest sends a sample notification.
	NotifyTest notifytest.Sender

	// Levels receives the resolved log level.
	Levels LevelSetter

	// Stdout receives command output.
	Stdout io.Writer

	// LookupEnv reads environment variables, normally [os.LookupEnv].
	LookupEnv func(key string) (string, bool)

	// Dirs are the user's XDG directories.
	Dirs config.Dirs

	// Version is the version line the version command prints.
	Version string
}
