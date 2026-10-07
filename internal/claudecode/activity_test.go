// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// activityWindow is the recent-change window the probe tests use.
const activityWindow = 2 * time.Minute

// probeNow is the current time in probe tests.
var probeNow = fixedModTime.Add(time.Hour)

// newTestProbe returns a probe over a new sessions directory whose process
// table is a second temporary directory.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - *ActivityProbe: the probe.
//   - string: the sessions directory.
//   - string: the fake process table.
func newTestProbe(t *testing.T) (*ActivityProbe, string, string) {
	t.Helper()

	sessions := t.TempDir()
	proc := t.TempDir()

	probe := NewActivityProbe(sessions, activityWindow)
	probe.procRoot = proc

	return probe, sessions, proc
}

// startProcess makes pid appear alive in the fake process table.
//
// Parameters:
//   - t: test handle.
//   - proc: the fake process table.
//   - pid: the process ID.
func startProcess(t *testing.T, proc string, pid int) {
	t.Helper()

	require.NoError(t, os.Mkdir(filepath.Join(proc, strconv.Itoa(pid)), 0o700))
}

// sessionJSON renders a session status file.
//
// Parameters:
//   - status: the session status, such as busy or idle.
//   - pid: the process ID written into the file.
//
// Returns:
//   - string: the file content.
func sessionJSON(status string, pid int) string {
	return `{"status":"` + status + `","pid":` + strconv.Itoa(pid) + `,"cwd":"/home/user/repo"}`
}

// TestNewActivityProbe starts without history and with the real process table.
func TestNewActivityProbe(t *testing.T) {
	t.Parallel()

	probe := NewActivityProbe("/sessions", time.Minute)

	assert.Equal(t, "/sessions", probe.dir)
	assert.Equal(t, time.Minute, probe.window)
	assert.Equal(t, "/proc", probe.procRoot)
	assert.Nil(t, probe.seen)
}

// TestActivityProbeMissingDirectory reports no activity and forgets history.
func TestActivityProbeMissingDirectory(t *testing.T) {
	t.Parallel()

	probe, sessions, _ := newTestProbe(t)
	writeFile(t, filepath.Join(sessions, "100.json"), sessionJSON("idle", 100), probeNow)

	require.True(t, probe.Active(probeNow))
	require.Len(t, probe.seen, 1)

	require.NoError(t, os.RemoveAll(sessions))

	assert.False(t, probe.Active(probeNow))
	assert.Nil(t, probe.seen)
}

// TestActivityProbeSessions covers recent changes and busy sessions.
func TestActivityProbeSessions(t *testing.T) {
	t.Parallel()

	const livePID = 4242

	stale := probeNow.Add(-time.Hour)

	tests := []struct {
		modTime time.Time
		name    string
		file    string
		content string
		want    bool
	}{
		{name: "recently changed", file: "100.json", content: sessionJSON("idle", 100), modTime: probeNow.Add(-time.Minute), want: true},
		{name: "changed at the window edge", file: "100.json", content: sessionJSON("idle", 100), modTime: probeNow.Add(-activityWindow), want: true},
		{name: "stale and idle", file: "100.json", content: sessionJSON("idle", livePID), modTime: stale},
		{name: "busy with a live pid", file: "100.json", content: sessionJSON("busy", livePID), modTime: stale, want: true},
		{name: "busy with a dead pid", file: "100.json", content: sessionJSON("busy", 999), modTime: stale},
		{name: "busy with the pid from the name", file: "4242.json", content: `{"status":"busy"}`, modTime: stale, want: true},
		{name: "busy with a zero pid", file: "0.json", content: `{"status":"busy","pid":0}`, modTime: stale},
		{name: "unparseable session", file: "4242.json", content: `{"status":"bu`, modTime: stale},
		{name: "non-numeric name", file: "abc.json", content: sessionJSON("busy", livePID), modTime: probeNow},
		{name: "key file", file: "4242.abc.key", content: sessionJSON("busy", livePID), modTime: probeNow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe, sessions, proc := newTestProbe(t)
			startProcess(t, proc, livePID)
			writeFile(t, filepath.Join(sessions, tt.file), tt.content, tt.modTime)

			assert.Equal(t, tt.want, probe.Active(probeNow))
		})
	}
}

// TestActivityProbeSkipsDirectories ignores a directory named like a session.
func TestActivityProbeSkipsDirectories(t *testing.T) {
	t.Parallel()

	probe, sessions, _ := newTestProbe(t)
	require.NoError(t, os.Mkdir(filepath.Join(sessions, "100.json"), 0o700))

	assert.False(t, probe.Active(probeNow))
	assert.Empty(t, probe.seen)
}

// TestActivityProbeNeverOpensKeyFiles never opens the secret ".key" files.
//
// The key file is a FIFO, so opening it for reading would block until a
// writer appears. Active runs in a goroutine and must finish quickly.
func TestActivityProbeNeverOpensKeyFiles(t *testing.T) {
	t.Parallel()

	probe, sessions, _ := newTestProbe(t)
	require.NoError(t, syscall.Mkfifo(filepath.Join(sessions, "123.abc.key"), 0o600))
	require.NoError(t, syscall.Mkfifo(filepath.Join(sessions, "124.json"), 0o600))
	writeFile(t, filepath.Join(sessions, "125.json"), sessionJSON("idle", 125), probeNow)

	done := make(chan bool, 1)

	go func() { done <- probe.Active(probeNow) }()

	select {
	case active := <-done:
		assert.True(t, active)
		assert.Len(t, probe.seen, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("Active blocked opening a FIFO")
	}
}

// TestActivityProbeReusesUnchangedSessions skips re-reading a session whose
// size and mtime are unchanged, and re-reads it once either changes.
func TestActivityProbeReusesUnchangedSessions(t *testing.T) {
	t.Parallel()

	const livePID = 4242

	probe, sessions, proc := newTestProbe(t)
	startProcess(t, proc, livePID)

	path := filepath.Join(sessions, "100.json")
	stale := probeNow.Add(-time.Hour)

	writeFile(t, path, sessionJSON("busy", livePID), stale)
	require.True(t, probe.Active(probeNow))

	// "idle" is as long as "busy", so size and mtime are unchanged.
	writeFile(t, path, sessionJSON("idle", livePID), stale)
	assert.True(t, probe.Active(probeNow), "unchanged session is not re-read")

	writeFile(t, path, sessionJSON("idle", livePID), stale.Add(time.Second))
	assert.False(t, probe.Active(probeNow), "changed session is re-read")
}

// TestActivityProbeAlive checks the process table.
func TestActivityProbeAlive(t *testing.T) {
	t.Parallel()

	probe, _, proc := newTestProbe(t)
	startProcess(t, proc, 7)

	assert.True(t, probe.alive(7))
	assert.False(t, probe.alive(8))
	assert.False(t, probe.alive(0))
	assert.False(t, probe.alive(-7))
}

// TestReadSession falls back to the file name for the pid and treats an
// unreadable file as idle.
func TestReadSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	tests := []struct {
		name    string
		file    string
		content string
		want    sessionInfo
	}{
		{name: "pid from the file", file: "10.json", content: sessionJSON("busy", 20), want: sessionInfo{pid: 20, busy: true}},
		{name: "pid from the name", file: "11.json", content: `{"status":"busy"}`, want: sessionInfo{pid: 11, busy: true}},
		{name: "negative pid in the file", file: "12.json", content: `{"status":"idle","pid":-3}`, want: sessionInfo{pid: 12}},
		{name: "invalid json", file: "13.json", content: `not json`, want: sessionInfo{pid: 13}},
		{name: "oversized", file: "14.json", content: `{"status":"busy"}` + strings.Repeat(" ", sessionLimit), want: sessionInfo{pid: 14}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(dir, tt.file)
			writeFile(t, path, tt.content, fixedModTime)

			info, err := os.Stat(path)
			require.NoError(t, err)

			got := readSession(path, tt.file, info)

			want := tt.want
			want.modTime, want.size = info.ModTime(), info.Size()
			assert.Equal(t, want, got)
		})
	}
}

// TestReadSessionVanished keeps the stat result when the file disappears
// between the directory scan and the read.
func TestReadSessionVanished(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "30.json")
	writeFile(t, path, sessionJSON("busy", 30), fixedModTime)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	got := readSession(path, "30.json", info)
	assert.Equal(t, sessionInfo{modTime: info.ModTime(), size: info.Size(), pid: 30}, got)
}
