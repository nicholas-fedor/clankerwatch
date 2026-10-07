// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package status

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/status/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/daemon"
)

// errResolve is returned by the failing resolver.
var errResolve = errors.New("resolve failed")

// failWriter fails every write so the error paths are reachable.
type failWriter struct{}

// Write always fails.
func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

// TestNewCommandMetadata checks the command, its JSON flag, and its arguments.
func TestNewCommandMetadata(t *testing.T) {
	t.Parallel()

	command := NewCommand(t.Context(), mocks.NewMockReporter(t), resolveTo(testConfig(), nil), &bytes.Buffer{})

	assert.Equal(t, "status", command.Name())
	assert.NotEmpty(t, command.Short)
	assert.NotEmpty(t, command.Long)
	assert.NotEmpty(t, command.Example)
	require.NotNil(t, command.Flags().Lookup(flagJSON))
	require.Error(t, command.Args(command, []string{"extra"}))
}

// TestRunWritesText checks the aligned table and the indented snapshot.
func TestRunWritesText(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	reporter := mocks.NewMockReporter(t)
	reporter.EXPECT().Status(t.Context(), cfg).Return(testReport(`{"v":1,"status":"ok"}`)).Once()

	var stdout bytes.Buffer

	command := NewCommand(t.Context(), reporter, resolveTo(cfg, nil), &stdout)
	command.SetArgs(nil)

	require.NoError(t, command.Execute())

	want := "version        v9\n" +
		"mode           cache-only\n" +
		"intervals      5m0s active, 20m0s idle\n" +
		"alerts         [80 95], login alerts true\n" +
		"credentials    /claude/.credentials.json\n" +
		"global config  /home/.claude.json\n" +
		"sessions       /claude/sessions\n" +
		"state file     /state/state.json\n" +
		"settings file  /config/clankerwatch/config.yaml\n" +
		"\nsnapshot:\n{\n  \"v\": 1,\n  \"status\": \"ok\"\n}\n"
	assert.Equal(t, want, stdout.String())
}

// TestRunWritesARawSnapshot checks a snapshot that is not JSON is still shown.
func TestRunWritesARawSnapshot(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	require.NoError(t, writeText(&stdout, testReport("  not json  ")))
	assert.Contains(t, stdout.String(), "\nsnapshot:\nnot json\n")
}

// TestRunWritesJSON checks the report decodes with the snapshot as an object.
//
// Scripts pipe the output to jq, so the snapshot must be embedded rather than
// quoted as a string.
func TestRunWritesJSON(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	reporter := mocks.NewMockReporter(t)
	reporter.EXPECT().Status(t.Context(), cfg).Return(testReport(`{"v":1,"status":"ok"}`)).Once()

	var stdout bytes.Buffer

	command := NewCommand(t.Context(), reporter, resolveTo(cfg, nil), &stdout)
	command.SetArgs([]string{"--json"})

	require.NoError(t, command.Execute())

	var decoded struct {
		Snapshot map[string]any `json:"snapshot"`
		Version  string         `json:"version"`
		Mode     string         `json:"mode"`
	}

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	assert.Equal(t, "v9", decoded.Version)
	assert.Equal(t, "cache-only", decoded.Mode)
	assert.Equal(t, map[string]any{"v": 1.0, "status": "ok"}, decoded.Snapshot)
	assert.Equal(t, byte('\n'), stdout.Bytes()[stdout.Len()-1])
}

// TestRunRejectsAnInvalidSnapshotAsJSON checks a broken snapshot is an error.
func TestRunRejectsAnInvalidSnapshotAsJSON(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	err := writeJSON(&stdout, testReport("{broken"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encode report")
	assert.Empty(t, stdout.String())
}

// TestRunReturnsTheResolveError checks nothing is read on bad settings.
func TestRunReturnsTheResolveError(t *testing.T) {
	t.Parallel()

	reporter := mocks.NewMockReporter(t)

	var stdout bytes.Buffer

	command := NewCommand(t.Context(), reporter, resolveTo(config.Config{}, errResolve), &stdout)

	require.ErrorIs(t, command.RunE(command, nil), errResolve)
	reporter.AssertNotCalled(t, "Status", mock.Anything, mock.Anything)
	assert.Empty(t, stdout.String())
}

// TestRunReportsWriteFailures checks both formats surface a closed stdout.
func TestRunReportsWriteFailures(t *testing.T) {
	t.Parallel()

	report := testReport(`{"v":1}`)

	require.ErrorIs(t, writeText(failWriter{}, report), options.ErrWriteOutput)
	require.ErrorIs(t, writeJSON(failWriter{}, report), options.ErrWriteOutput)
}

// testConfig returns settings for the report doubles.
func testConfig() config.Config {
	return config.Config{
		Mode:         config.ModeCacheOnly,
		Home:         "/home",
		StateDir:     "/state",
		Thresholds:   []float64{80, 95},
		Interval:     config.DefaultInterval,
		IdleInterval: config.DefaultIdleInterval,
		NotifyAuth:   true,
	}
}

// testReport returns a report carrying snapshot.
func testReport(snapshot string) daemon.Report {
	return daemon.Report{
		Version:      "v9",
		Mode:         config.ModeCacheOnly,
		Credentials:  "/claude/.credentials.json",
		GlobalConfig: "/home/.claude.json",
		Sessions:     "/claude/sessions",
		StateFile:    "/state/state.json",
		SettingsFile: "/config/clankerwatch/config.yaml",
		Snapshot:     json.RawMessage(snapshot),
		Thresholds:   []float64{80, 95},
		Interval:     5 * time.Minute,
		IdleInterval: 20 * time.Minute,
		NotifyAuth:   true,
	}
}

// resolveTo returns a resolver with a fixed result.
func resolveTo(cfg config.Config, err error) func() (config.Config, error) {
	return func() (config.Config, error) { return cfg, err }
}
