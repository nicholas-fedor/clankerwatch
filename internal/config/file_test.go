// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exampleFile is the settings file `task install` and the packages ship.
const exampleFile = "../../build/package/config.example.yaml"

// writeSettings writes content to config.yaml in a temporary directory.
//
// Parameters:
//   - t: test handle.
//   - content: the file content.
//
// Returns:
//   - string: the file path.
func writeSettings(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// TestLoadFileMissing lets only an optional file be absent.
//
// The default path need not exist, but a file the user names must, so a
// mistyped --config path never falls back to defaults unnoticed.
func TestLoadFileMissing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "absent.yaml")

	got, err := LoadOptionalFile(path)
	require.NoError(t, err)
	assert.Equal(t, File{}, got)

	_, err = LoadFile(path)
	require.ErrorIs(t, err, ErrInvalidFile)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestLoadFileReadsSettings decodes every key from disk.
func TestLoadFileReadsSettings(t *testing.T) {
	t.Parallel()

	path := writeSettings(t, `mode: cache-only
interval: 7m
idleInterval: 1h
notify: [90, 50]
notifyAuth: false
logLevel: debug
claudeConfigDir: /claude
stateDir: /state
`)

	got, err := LoadFile(path)
	require.NoError(t, err)

	require.NotNil(t, got.Mode)
	assert.Equal(t, "cache-only", *got.Mode)
	require.NotNil(t, got.Interval)
	assert.Equal(t, 7*time.Minute, *got.Interval)
	require.NotNil(t, got.IdleInterval)
	assert.Equal(t, time.Hour, *got.IdleInterval)
	require.NotNil(t, got.Notify)
	assert.Equal(t, []float64{90, 50}, *got.Notify)
	require.NotNil(t, got.NotifyAuth)
	assert.False(t, *got.NotifyAuth)
	require.NotNil(t, got.LogLevel)
	assert.Equal(t, "debug", *got.LogLevel)
	require.NotNil(t, got.ClaudeConfigDir)
	assert.Equal(t, "/claude", *got.ClaudeConfigDir)
	require.NotNil(t, got.StateDir)
	assert.Equal(t, "/state", *got.StateDir)
}

// TestLoadFileNamesThePath puts the file path in decode errors.
func TestLoadFileNamesThePath(t *testing.T) {
	t.Parallel()

	path := writeSettings(t, "intervall: 5m\n")

	_, err := LoadOptionalFile(path)
	require.ErrorIs(t, err, ErrInvalidFile)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "intervall")
}

// TestLoadFileTooLarge refuses a file above the size limit.
func TestLoadFileTooLarge(t *testing.T) {
	t.Parallel()

	path := writeSettings(t, "#"+strings.Repeat("x", maxFileSize)+"\n")

	_, err := LoadOptionalFile(path)
	require.ErrorIs(t, err, ErrInvalidFile)
	require.ErrorIs(t, err, ErrFileTooLarge)
}

// TestLoadFileUnreadable reports a path that cannot be read as a file.
func TestLoadFileUnreadable(t *testing.T) {
	t.Parallel()

	_, err := LoadOptionalFile(t.TempDir())
	require.ErrorIs(t, err, ErrInvalidFile)
}

// TestDecodeFileEmpty treats content without a document as no settings.
//
// The installed example is all comments, so it must load as an empty file.
func TestDecodeFileEmpty(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"", "\n\n", "# only comments\n", "~\n", "---\n", "...\n"} {
		got, err := DecodeFile([]byte(content))
		require.NoError(t, err, "%q", content)
		assert.Equal(t, File{}, got, "%q", content)
	}
}

// TestDecodeFileRejects covers content the strict decoder refuses.
func TestDecodeFileRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "unknown key", content: "bogus: 1\n"},
		{name: "snake case key", content: "idle_interval: 20m\n"},
		{name: "bare number duration", content: "interval: 5\n"},
		{name: "unparsable duration", content: "interval: soon\n"},
		{name: "scalar thresholds", content: "notify: 80\n"},
		{name: "text threshold", content: "notify: [high]\n"},
		{name: "duplicate key", content: "mode: hybrid\nmode: cache-only\n"},
		{name: "sequence document", content: "[1, 2]\n"},
		{name: "malformed", content: "notify: [80\n"},
		{name: "second document", content: "interval: 5m\n---\ninterval: 9m\n"},
		{name: "malformed second document", content: "interval: 5m\n---\nnotify: [\n"},
		{name: "null key", content: "~: 1\n"},
		{name: "empty explicit key", content: "? \n"},
		{name: "merge key document", content: "<<\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecodeFile([]byte(test.content))
			require.ErrorIs(t, err, ErrInvalidFile)
			assert.Equal(t, File{}, got)
		})
	}
}

// TestDecodeFileSecondDocument names the multiple-document error.
func TestDecodeFileSecondDocument(t *testing.T) {
	t.Parallel()

	_, err := DecodeFile([]byte("interval: 5m\n---\ninterval: 9m\n"))
	require.ErrorIs(t, err, ErrMultipleDocuments)
}

// TestDecodeFileTrailingEmptyDocuments ignores empty documents after the first.
func TestDecodeFileTrailingEmptyDocuments(t *testing.T) {
	t.Parallel()

	got, err := DecodeFile([]byte("---\ninterval: 5m\n---\n---\n~\n"))
	require.NoError(t, err)
	require.NotNil(t, got.Interval)
	assert.Equal(t, 5*time.Minute, *got.Interval)
}

// TestDecodeFileNullKeepsDefault leaves a key with no value absent.
//
// Only an explicit empty list turns alerts off.
func TestDecodeFileNullKeepsDefault(t *testing.T) {
	t.Parallel()

	got, err := DecodeFile([]byte("notify:\n"))
	require.NoError(t, err)
	assert.Nil(t, got.Notify)

	got, err = DecodeFile([]byte("notify: []\n"))
	require.NoError(t, err)
	require.NotNil(t, got.Notify)
	assert.Empty(t, *got.Notify)
}

// TestApplyEveryKey copies each present key onto the settings.
func TestApplyEveryKey(t *testing.T) {
	t.Parallel()

	file, err := DecodeFile([]byte(`mode: cache-only
interval: 3m
idleInterval: 30m
notify: [95, 50, 95]
notifyAuth: false
logLevel: WARN
claudeConfigDir: /claude
stateDir: /state
`))
	require.NoError(t, err)

	cfg := validConfig(t)
	require.NoError(t, file.Apply(&cfg))

	assert.Equal(t, ModeCacheOnly, cfg.Mode)
	assert.Equal(t, 3*time.Minute, cfg.Interval)
	assert.Equal(t, 30*time.Minute, cfg.IdleInterval)
	assert.Equal(t, []float64{50, 95}, cfg.Thresholds)
	assert.False(t, cfg.NotifyAuth)
	assert.Equal(t, slog.LevelWarn, cfg.LogLevel)
	assert.Equal(t, "/claude", cfg.ClaudeDir)
	assert.Equal(t, "/state", cfg.StateDir)
}

// TestApplyEmptyKeepsSettings changes nothing for a file without keys.
func TestApplyEmptyKeepsSettings(t *testing.T) {
	t.Parallel()

	cfg := validConfig(t)
	want := cfg

	require.NoError(t, File{}.Apply(&cfg))
	assert.Equal(t, want, cfg)
}

// TestApplyEmptyStateDirKeepsDefault treats an empty stateDir as unset, like
// the flag and the environment variable.
func TestApplyEmptyStateDirKeepsDefault(t *testing.T) {
	t.Parallel()

	empty := ""
	cfg := validConfig(t)
	want := cfg.StateDir

	require.NoError(t, File{StateDir: &empty}.Apply(&cfg))
	assert.Equal(t, want, cfg.StateDir)
}

// TestApplyEmptyNotifyDisablesAlerts turns alerts off for an empty list.
func TestApplyEmptyNotifyDisablesAlerts(t *testing.T) {
	t.Parallel()

	none := []float64{}
	cfg := validConfig(t)

	require.NoError(t, File{Notify: &none}.Apply(&cfg))
	assert.Empty(t, cfg.Thresholds)
}

// TestApplyRejects names the key whose value cannot be used.
func TestApplyRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want    error
		name    string
		content string
		key     string
	}{
		{name: "mode", content: "mode: online\n", key: "mode", want: ErrInvalidMode},
		{name: "threshold", content: "notify: [80, 101]\n", key: "notify", want: ErrInvalidThreshold},
		{name: "nan threshold", content: "notify: [.nan]\n", key: "notify", want: ErrInvalidThreshold},
		{name: "log level", content: "logLevel: loud\n", key: "logLevel", want: ErrInvalidLogLevel},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			file, err := DecodeFile([]byte(test.content))
			require.NoError(t, err)

			cfg := validConfig(t)
			err = file.Apply(&cfg)
			require.ErrorIs(t, err, test.want)
			assert.True(t, strings.HasPrefix(err.Error(), test.key+": "), "error %q names %s", err, test.key)
		})
	}
}

// TestExampleFileMatchesDefaults keeps the shipped example honest.
//
// The example documents each key as a commented-out line with its default.
// Uncommenting every such line must give a file the strict decoder accepts
// and that leaves the defaults unchanged, so the example never drifts from
// the code.
func TestExampleFileMatchesDefaults(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(exampleFile)
	require.NoError(t, err)

	commented, err := DecodeFile(data)
	require.NoError(t, err)
	assert.Equal(t, File{}, commented, "every key in the example is commented out")

	uncommented := regexp.MustCompile(`(?m)^#([a-zA-Z]+:)`).ReplaceAll(data, []byte("$1"))

	file, err := DecodeFile(uncommented)
	require.NoError(t, err)

	keys := map[string]bool{
		"mode": file.Mode != nil, "interval": file.Interval != nil, "idleInterval": file.IdleInterval != nil,
		"notify": file.Notify != nil, "notifyAuth": file.NotifyAuth != nil, "logLevel": file.LogLevel != nil,
		"claudeConfigDir": file.ClaudeConfigDir != nil, "stateDir": file.StateDir != nil,
	}
	for key, present := range keys {
		assert.True(t, present, "the example documents %s", key)
	}

	cfg := validConfig(t)
	want := cfg

	require.NoError(t, file.Apply(&cfg))
	assert.Equal(t, want, cfg)
}
