// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewLoggerStartsAtInfo checks debug is hidden until the level drops.
//
// The logger is wired into every service before cobra parses --log-level, so
// SetLevel must change the level of the logger already in use.
func TestNewLoggerStartsAtInfo(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer

	log := newLogger(&sink, envMap(nil))

	log.Debug(t.Context(), "hidden debug", "key", "value")
	assert.Empty(t, sink.String())

	log.Info(t.Context(), "an info line", "key", "value")
	log.Warn(t.Context(), "a warn line", "key", "value")
	assert.Contains(t, sink.String(), `level=INFO msg="an info line" key=value`)
	assert.Contains(t, sink.String(), `level=WARN msg="a warn line" key=value`)

	sink.Reset()
	log.SetLevel(slog.LevelDebug)
	log.Debug(t.Context(), "shown debug")
	assert.Contains(t, sink.String(), `level=DEBUG msg="shown debug"`)

	sink.Reset()
	log.SetLevel(slog.LevelError)
	log.Warn(t.Context(), "hidden warn")
	assert.Empty(t, sink.String())
}

// TestNewLoggerTimestamps checks timestamps are dropped only for the journal.
//
// journald stamps every line itself, so a second timestamp is noise there but
// needed on a terminal.
func TestNewLoggerTimestamps(t *testing.T) {
	t.Parallel()

	var terminal, journal bytes.Buffer

	newLogger(&terminal, envMap(nil)).Info(t.Context(), "terminal line")
	newLogger(&journal, envMap(map[string]string{envJournalStream: "8:12345"})).Info(t.Context(), "journal line")

	assert.Contains(t, terminal.String(), "time=")
	assert.NotContains(t, journal.String(), "time=")
	assert.Contains(t, journal.String(), `msg="journal line"`)
}

// TestDropTime checks only the top-level time attribute is removed.
func TestDropTime(t *testing.T) {
	t.Parallel()

	timeAttr := slog.String(slog.TimeKey, "now")
	other := slog.String(slog.MessageKey, "value")

	assert.Equal(t, slog.Attr{}, dropTime(nil, timeAttr))
	assert.Equal(t, timeAttr, dropTime([]string{"group"}, timeAttr))
	assert.Equal(t, other, dropTime(nil, other))
}
