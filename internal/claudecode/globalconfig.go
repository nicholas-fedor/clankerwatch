// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"
)

// CachedUsage is the usage payload Claude Code saved after its own fetch.
type CachedUsage struct {
	// FetchedAt is when Claude Code fetched the payload.
	FetchedAt time.Time

	// AccountUUID is the account the payload belongs to.
	AccountUUID string

	// Payload is the raw usage payload.
	Payload json.RawMessage
}

// GlobalState is what the daemon needs from Claude Code's global config.
type GlobalState struct {
	// Cached is Claude Code's cached usage, or nil.
	Cached *CachedUsage

	// AccountUUID is the signed-in account.
	AccountUUID string
}

// GlobalConfigReader reads Claude Code's global config and re-reads it only
// after it changes.
type GlobalConfigReader struct {
	state GlobalState
	path  string
	id    fileID
	read  bool
}

// globalConfigFile lists the only fields the daemon decodes.
//
//nolint:tagliatelle // Claude Code fixes these names on disk.
type globalConfigFile struct {
	OAuthAccount *oauthAccount `json:"oauthAccount"`
	Cached       *cachedUsage  `json:"cachedUsageUtilization"`
}

// oauthAccount is the signed-in account in the global config.
type oauthAccount struct {
	AccountUUID string `json:"accountUuid"`
}

// cachedUsage is Claude Code's cached usage in the global config.
type cachedUsage struct {
	AccountUUID string          `json:"accountUuid"`
	Utilization json.RawMessage `json:"utilization"`
	FetchedAtMs float64         `json:"fetchedAtMs"`
}

// globalConfigLimit is the largest global config file the reader accepts.
const globalConfigLimit = 16 << 20

// NewGlobalConfigReader returns a reader for the global config at path.
//
// Parameters:
//   - path: the global config file.
//
// Returns:
//   - *GlobalConfigReader: a reader that has not read the file yet.
func NewGlobalConfigReader(path string) *GlobalConfigReader {
	return &GlobalConfigReader{path: path, id: fileID{}, read: false, state: GlobalState{AccountUUID: "", Cached: nil}}
}

// Read returns the current state.
//
// A missing file yields an empty state. Claude Code rewrites the file often,
// so a parse error keeps the previous state and the file is read again on the
// next call.
//
// Returns:
//   - GlobalState: the current, or previous, state.
//   - error: a read or parse error.
func (r *GlobalConfigReader) Read() (GlobalState, error) {
	id, err := statID(r.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			r.read, r.state = false, GlobalState{AccountUUID: "", Cached: nil}

			return r.state, nil
		}

		return r.state, fmt.Errorf("check global config: %w", err)
	}

	if r.read && id == r.id {
		return r.state, nil
	}

	state, err := readGlobalConfig(r.path)
	if err != nil {
		return r.state, err
	}

	r.state, r.id, r.read = state, id, true

	return state, nil
}

// UsableCache returns Claude Code's cached usage when it belongs to the
// signed-in account.
//
// Returns:
//   - CachedUsage: the cached usage.
//   - bool: false without a cache for the signed-in account.
func (g GlobalState) UsableCache() (CachedUsage, bool) {
	if g.Cached == nil || g.Cached.AccountUUID == "" || g.Cached.AccountUUID != g.AccountUUID {
		return CachedUsage{FetchedAt: time.Time{}, AccountUUID: "", Payload: nil}, false
	}

	return *g.Cached, true
}

// readGlobalConfig parses the global config file.
//
// Parameters:
//   - path: the global config file.
//
// Returns:
//   - GlobalState: the decoded state.
//   - error: a read or parse error.
func readGlobalConfig(path string) (GlobalState, error) {
	content, err := readLimited(path, globalConfigLimit)
	if err != nil {
		return GlobalState{AccountUUID: "", Cached: nil}, fmt.Errorf("read global config: %w", err)
	}

	defer clear(content)

	var file globalConfigFile

	err = json.Unmarshal(content, &file)
	if err != nil {
		return GlobalState{AccountUUID: "", Cached: nil}, fmt.Errorf("parse global config: %w", err)
	}

	state := GlobalState{AccountUUID: "", Cached: nil}
	if file.OAuthAccount != nil {
		state.AccountUUID = file.OAuthAccount.AccountUUID
	}

	cached := file.Cached
	if cached != nil && cached.FetchedAtMs > 0 && len(cached.Utilization) > 0 && string(cached.Utilization) != "null" {
		state.Cached = &CachedUsage{
			FetchedAt:   time.UnixMilli(int64(cached.FetchedAtMs)),
			AccountUUID: cached.AccountUUID,
			Payload:     append(json.RawMessage(nil), cached.Utilization...),
		}
	}

	return state, nil
}
