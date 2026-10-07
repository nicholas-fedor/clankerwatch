// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"io"
	"log/slog"
)

// logger adapts slog to the packages' logger interfaces.
//
// Messages stay static at the call site and args are key-value pairs. The
// level is a [slog.LevelVar], so the command tree can set it after parsing
// even though every service was wired with this logger before.
type logger struct {
	sink  *slog.Logger
	level *slog.LevelVar
}

// envJournalStream is set by systemd when stderr goes to the journal.
const envJournalStream = "JOURNAL_STREAM"

// newLogger returns a logger that writes text to writer.
//
// When stderr goes to the journal, timestamps are dropped because the journal
// records its own.
//
// Parameters:
//   - writer: the diagnostic stream.
//   - lookupEnv: reads environment variables.
//
// Returns:
//   - *logger: a logger at info level.
func newLogger(writer io.Writer, lookupEnv func(string) (string, bool)) *logger {
	level := new(slog.LevelVar)

	options := &slog.HandlerOptions{AddSource: false, Level: level, ReplaceAttr: nil}
	if _, journald := lookupEnv(envJournalStream); journald {
		options.ReplaceAttr = dropTime
	}

	return &logger{sink: slog.New(slog.NewTextHandler(writer, options)), level: level}
}

// Debug records a debug event.
//
// Parameters:
//   - ctx: cancellation and request scope.
//   - msg: static message.
//   - args: key-value pairs.
func (l *logger) Debug(ctx context.Context, msg string, args ...any) {
	//nolint:sloglint // Callers pass static messages and constant snake_case keys.
	l.sink.DebugContext(ctx, msg, args...)
}

// Info records an informational event.
//
// Parameters:
//   - ctx: cancellation and request scope.
//   - msg: static message.
//   - args: key-value pairs.
func (l *logger) Info(ctx context.Context, msg string, args ...any) {
	//nolint:sloglint // Callers pass static messages and constant snake_case keys.
	l.sink.InfoContext(ctx, msg, args...)
}

// SetLevel sets the minimum level.
//
// Parameters:
//   - level: the minimum level.
func (l *logger) SetLevel(level slog.Level) {
	l.level.Set(level)
}

// Warn records a recoverable event.
//
// Parameters:
//   - ctx: cancellation and request scope.
//   - msg: static message.
//   - args: key-value pairs.
func (l *logger) Warn(ctx context.Context, msg string, args ...any) {
	//nolint:sloglint // Callers pass static messages and constant snake_case keys.
	l.sink.WarnContext(ctx, msg, args...)
}

// dropTime removes the timestamp attribute.
//
// Parameters:
//   - groups: the attribute's groups.
//   - attr: the attribute.
//
// Returns:
//   - [slog.Attr]: an empty attribute for the top-level time, otherwise attr.
func dropTime(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 && attr.Key == slog.TimeKey {
		return slog.Attr{}
	}

	return attr
}
