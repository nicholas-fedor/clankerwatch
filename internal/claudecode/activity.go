// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ActivityProbe guesses whether Claude Code is in use from its session status
// files. It only steers how often the daemon polls.
type ActivityProbe struct {
	seen     map[string]sessionInfo
	dir      string
	procRoot string
	window   time.Duration
}

// sessionInfo is what the probe remembers about one session file.
type sessionInfo struct {
	modTime time.Time
	size    int64
	pid     int
	busy    bool
}

// sessionStatus lists the fields read from a session file.
type sessionStatus struct {
	Status string `json:"status"`
	PID    int    `json:"pid"`
}

const (
	// sessionLimit is the largest session file the probe reads.
	sessionLimit = 64 << 10

	// sessionSuffix ends every session file name.
	sessionSuffix = ".json"

	// statusBusy marks a session in the middle of a turn.
	statusBusy = "busy"

	// procRoot is where live processes are listed.
	procRoot = "/proc"
)

// sessionFile matches the session status files. The ".key" files next to
// them hold secrets and are never opened.
var sessionFile = regexp.MustCompile(`^\d+\.json$`)

// NewActivityProbe watches the session files in dir.
//
// Parameters:
//   - dir: Claude Code's sessions directory.
//   - window: how recently a file must have changed to count as activity.
//
// Returns:
//   - *ActivityProbe: a probe with no history.
func NewActivityProbe(dir string, window time.Duration) *ActivityProbe {
	return &ActivityProbe{dir: dir, window: window, procRoot: procRoot, seen: nil}
}

// Active reports whether a session changed recently or is busy in a live
// process.
//
// Parameters:
//   - now: the current time.
//
// Returns:
//   - bool: true while Claude Code appears to be in use.
func (p *ActivityProbe) Active(now time.Time) bool {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		p.seen = nil

		return false
	}

	active := false
	next := make(map[string]sessionInfo, len(entries))

	for _, entry := range entries {
		info, ok := p.observe(entry)
		if !ok {
			continue
		}

		next[entry.Name()] = info

		if now.Sub(info.modTime) <= p.window || (info.busy && p.alive(info.pid)) {
			active = true
		}
	}

	p.seen = next

	return active
}

// alive reports whether a process with pid exists.
//
// Parameters:
//   - pid: the process ID.
//
// Returns:
//   - bool: true for a live process.
func (p *ActivityProbe) alive(pid int) bool {
	if pid <= 0 {
		return false
	}

	_, err := os.Stat(filepath.Join(p.procRoot, strconv.Itoa(pid)))

	return err == nil
}

// observe returns the state of one directory entry, re-reading the file only
// when it changed.
//
// Parameters:
//   - entry: an entry of the sessions directory.
//
// Returns:
//   - sessionInfo: the session's state.
//   - bool: false for entries that are not session files.
func (p *ActivityProbe) observe(entry os.DirEntry) (sessionInfo, bool) {
	name := entry.Name()
	if !sessionFile.MatchString(name) {
		return sessionInfo{}, false
	}

	info, err := entry.Info()
	if err != nil || !info.Mode().IsRegular() {
		return sessionInfo{}, false
	}

	previous, ok := p.seen[name]
	if ok && previous.modTime.Equal(info.ModTime()) && previous.size == info.Size() {
		return previous, true
	}

	return readSession(filepath.Join(p.dir, name), name, info), true
}

// readSession reads one session file.
//
// The process ID falls back to the file name when the file does not say.
//
// Parameters:
//   - path: the session file.
//   - name: the file name.
//   - info: the file's stat result.
//
// Returns:
//   - sessionInfo: the session's state.
func readSession(path, name string, info os.FileInfo) sessionInfo {
	session := sessionInfo{modTime: info.ModTime(), size: info.Size(), pid: 0, busy: false}

	pid, err := strconv.Atoi(strings.TrimSuffix(name, sessionSuffix))
	if err == nil {
		session.pid = pid
	}

	content, err := readLimited(path, sessionLimit)
	if err != nil {
		return session
	}

	var status sessionStatus

	if json.Unmarshal(content, &status) == nil {
		if status.PID > 0 {
			session.pid = status.PID
		}

		session.busy = status.Status == statusBusy
	}

	return session
}
