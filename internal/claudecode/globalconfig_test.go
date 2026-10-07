// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture values for the global config.
const (
	// testAccount is the signed-in account in global config fixtures.
	testAccount = "4f6a2b1c-0000-4000-8000-000000000001"

	// otherAccount is an account that is not signed in.
	otherAccount = "4f6a2b1c-0000-4000-8000-000000000002"

	// testFetchedMs is when the cached usage was fetched, in Unix milliseconds.
	testFetchedMs int64 = 1_783_000_000_000

	// testUtilization is a cached usage payload.
	testUtilization = `{"five_hour":{"utilization":42}}`
)

// globalConfigJSON renders a global config the way Claude Code writes it.
//
// Parameters:
//   - account: the signed-in account.
//   - cachedAccount: the account the cached usage belongs to.
//
// Returns:
//   - string: the file content, including fields the reader must skip.
func globalConfigJSON(account, cachedAccount string) string {
	return fmt.Sprintf(`{
  "numStartups": 12,
  "projects": {"/home/user/repo": {"allowedTools": []}},
  "oauthAccount": {"accountUuid": %q, "emailAddress": "user@example.com"},
  "cachedUsageUtilization": {"accountUuid": %q, "fetchedAtMs": %d, "utilization": %s}
}`, account, cachedAccount, testFetchedMs, testUtilization)
}

// signedInState is the state globalConfigJSON(testAccount, testAccount) decodes to.
//
// Returns:
//   - GlobalState: the expected state.
func signedInState() GlobalState {
	return GlobalState{
		AccountUUID: testAccount,
		Cached: &CachedUsage{
			FetchedAt:   time.UnixMilli(testFetchedMs),
			AccountUUID: testAccount,
			Payload:     json.RawMessage(testUtilization),
		},
	}
}

// TestReadGlobalConfig covers the decoded fields and the ignored caches.
func TestReadGlobalConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    GlobalState
		failure bool
	}{
		{name: "signed in with cache", content: globalConfigJSON(testAccount, testAccount), want: signedInState()},
		{name: "empty object", content: `{}`},
		{name: "signed in without cache", content: `{"oauthAccount":{"accountUuid":"a"}}`, want: GlobalState{AccountUUID: "a"}},
		{
			name:    "null utilization",
			content: `{"cachedUsageUtilization":{"accountUuid":"a","fetchedAtMs":1,"utilization":null}}`,
		},
		{
			name:    "missing utilization",
			content: `{"cachedUsageUtilization":{"accountUuid":"a","fetchedAtMs":1}}`,
		},
		{
			name:    "missing fetch time",
			content: `{"cachedUsageUtilization":{"accountUuid":"a","utilization":{}}}`,
		},
		{name: "truncated", content: `{"oauthAccount":{"accountUu`, failure: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), globalConfigName)
			writeFile(t, path, tt.content, fixedModTime)

			got, err := readGlobalConfig(path)
			if tt.failure {
				require.Error(t, err)
				assert.Equal(t, GlobalState{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestReadGlobalConfigTooLarge rejects a file over the size limit.
func TestReadGlobalConfigTooLarge(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), globalConfigName)
	writeFile(t, path, strings.Repeat(" ", globalConfigLimit+1), fixedModTime)

	_, err := readGlobalConfig(path)
	require.ErrorIs(t, err, errTooLarge)
}

// TestGlobalConfigReaderMissingFile yields an empty state without an error.
func TestGlobalConfigReaderMissingFile(t *testing.T) {
	t.Parallel()

	reader := NewGlobalConfigReader(filepath.Join(t.TempDir(), globalConfigName))

	got, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, GlobalState{}, got)
}

// TestGlobalConfigReaderForgetsDeletedFile drops the previous state once the
// file is gone.
func TestGlobalConfigReaderForgetsDeletedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), globalConfigName)
	writeFile(t, path, globalConfigJSON(testAccount, testAccount), fixedModTime)

	reader := NewGlobalConfigReader(path)

	got, err := reader.Read()
	require.NoError(t, err)
	require.Equal(t, signedInState(), got)

	require.NoError(t, os.Remove(path))

	got, err = reader.Read()
	require.NoError(t, err)
	assert.Equal(t, GlobalState{}, got)
}

// TestGlobalConfigReaderStatError keeps the previous state and reports the
// stat failure.
func TestGlobalConfigReaderStatError(t *testing.T) {
	t.Parallel()

	reader := NewGlobalConfigReader(notDirPath(t))

	got, err := reader.Read()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check global config")
	assert.Equal(t, GlobalState{}, got)
}

// TestGlobalConfigReaderKeepsStateOnTruncatedFile returns the previous state
// while Claude Code is midway through a rewrite.
func TestGlobalConfigReaderKeepsStateOnTruncatedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), globalConfigName)
	full := globalConfigJSON(testAccount, testAccount)
	writeFile(t, path, full, fixedModTime)

	reader := NewGlobalConfigReader(path)

	_, err := reader.Read()
	require.NoError(t, err)

	writeFile(t, path, full[:len(full)/2], fixedModTime.Add(time.Second))

	got, err := reader.Read()
	require.Error(t, err)
	assert.Equal(t, signedInState(), got)

	// The truncated version is not cached, so the next call reads again.
	got, err = reader.Read()
	require.Error(t, err)
	assert.Equal(t, signedInState(), got)

	replaceFile(t, path, globalConfigJSON(otherAccount, otherAccount), fixedModTime.Add(2*time.Second))

	got, err = reader.Read()
	require.NoError(t, err)
	assert.Equal(t, otherAccount, got.AccountUUID)
}

// TestGlobalConfigReaderSkipsUnchangedFile returns the cached state while the
// file identity is unchanged.
func TestGlobalConfigReaderSkipsUnchangedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), globalConfigName)
	writeFile(t, path, globalConfigJSON(testAccount, testAccount), fixedModTime)

	reader := NewGlobalConfigReader(path)

	_, err := reader.Read()
	require.NoError(t, err)

	writeFile(t, path, globalConfigJSON(otherAccount, otherAccount), fixedModTime)

	got, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, signedInState(), got)

	replaceFile(t, path, globalConfigJSON(otherAccount, otherAccount), fixedModTime)

	got, err = reader.Read()
	require.NoError(t, err)
	assert.Equal(t, otherAccount, got.AccountUUID)
}

// TestGlobalStateUsableCache requires the cache to belong to the signed-in
// account.
func TestGlobalStateUsableCache(t *testing.T) {
	t.Parallel()

	cached := signedInState().Cached

	tests := []struct {
		name  string
		state GlobalState
		want  bool
	}{
		{name: "matching account", state: GlobalState{AccountUUID: testAccount, Cached: cached}, want: true},
		{name: "other account", state: GlobalState{AccountUUID: otherAccount, Cached: cached}},
		{name: "signed out", state: GlobalState{Cached: cached}},
		{name: "no cache", state: GlobalState{AccountUUID: testAccount}},
		{
			name:  "cache without account",
			state: GlobalState{Cached: &CachedUsage{FetchedAt: time.UnixMilli(testFetchedMs)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tt.state.UsableCache()
			assert.Equal(t, tt.want, ok)

			if tt.want {
				assert.Equal(t, *cached, got)
			} else {
				assert.Equal(t, CachedUsage{}, got)
			}
		})
	}
}
