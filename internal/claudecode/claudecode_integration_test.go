// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package claudecode_test exercises the Claude Code readers through public APIs
// against fixture homes built in temporary directories.
package claudecode_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
)

// Fixture values shared by the integration flows.
const (
	// accountA is the account signed in first.
	accountA = "0b5e7a10-1111-4111-8111-111111111111"

	// accountB is the account signed in after switching.
	accountB = "0b5e7a10-2222-4222-8222-222222222222"

	// firstToken is the access token before Claude Code refreshes it.
	firstToken = "sk-ant-oat01-first-token-value"

	// refreshedToken has the same length as firstToken, so the refreshed file
	// keeps its size.
	refreshedToken = "sk-ant-oat01-fresh-token-value"
)

// flowTime is the pinned modification time for every fixture file.
var flowTime = time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)

// saveLikeClaudeCode writes content the way Claude Code saves its files: into
// a temporary file in the same directory, renamed over the target.
//
// Parameters:
//   - t: test handle.
//   - path: the target file.
//   - content: the new content.
func saveLikeClaudeCode(t *testing.T, path, content string) {
	t.Helper()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	require.NoError(t, err)

	_, err = tmp.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmp.Close())
	require.NoError(t, os.Chtimes(tmp.Name(), flowTime, flowTime))
	require.NoError(t, os.Rename(tmp.Name(), path))
}

// credentialsFor renders a credentials file holding token.
//
// Parameters:
//   - token: the access token.
//
// Returns:
//   - string: the file content.
func credentialsFor(token string) string {
	return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"refreshToken":"sk-ant-ort01-refresh",`+
		`"expiresAt":1784000000000,"refreshTokenExpiresAt":1790000000000,"scopes":["user:inference","user:profile"],`+
		`"subscriptionType":"max","rateLimitTier":"default_claude_max_5x"}}`, token)
}

// globalConfigFor renders a global config with a signed-in account and a cache
// fetched for cachedAccount.
//
// Parameters:
//   - account: the signed-in account.
//   - cachedAccount: the account the cache was fetched for.
//
// Returns:
//   - string: the file content.
func globalConfigFor(account, cachedAccount string) string {
	return fmt.Sprintf(`{"numStartups":3,"oauthAccount":{"accountUuid":%q},`+
		`"cachedUsageUtilization":{"accountUuid":%q,"fetchedAtMs":1783000000000,`+
		`"utilization":{"five_hour":{"utilization":42}}}}`, account, cachedAccount)
}

// newFixtureHome builds a signed-in Claude Code layout in a temporary home.
//
// Parameters:
//   - t: test handle.
//   - configDir: the CLAUDE_CONFIG_DIR value, or empty for the home layout.
//   - home: the home directory.
//
// Returns:
//   - claudecode.Paths: the resolved locations, with every file in place.
func newFixtureHome(t *testing.T, configDir, home string) claudecode.Paths {
	t.Helper()

	paths := claudecode.ResolvePaths(configDir, home)
	require.NoError(t, os.MkdirAll(paths.Sessions, 0o700))

	saveLikeClaudeCode(t, paths.Credentials, credentialsFor(firstToken))
	saveLikeClaudeCode(t, paths.GlobalConfig, globalConfigFor(accountA, accountA))

	return paths
}

// TestHomeLayoutFlow reads a signed-in home, follows a token refresh and an
// account switch, and probes session activity.
func TestHomeLayoutFlow(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	paths := newFixtureHome(t, "", home)

	assert.Equal(t, filepath.Join(home, ".claude", ".credentials.json"), paths.Credentials)
	assert.Equal(t, filepath.Join(home, ".claude.json"), paths.GlobalConfig)

	credentials := claudecode.NewCredentialsReader(paths.Credentials)
	global := claudecode.NewGlobalConfigReader(paths.GlobalConfig)

	oauth, err := credentials.Read()
	require.NoError(t, err)
	assert.Equal(t, firstToken, oauth.AccessToken.Reveal())
	assert.True(t, oauth.HasScope("user:profile"))
	assert.True(t, oauth.UsableAt(flowTime, 5*time.Minute))
	assert.Equal(t, "max", oauth.SubscriptionType)

	state, err := global.Read()
	require.NoError(t, err)

	cache, ok := state.UsableCache()
	require.True(t, ok)
	assert.Equal(t, accountA, cache.AccountUUID)
	assert.JSONEq(t, `{"five_hour":{"utilization":42}}`, string(cache.Payload))

	// Claude Code refreshes the token. The new file has the same size and
	// mtime, so only the inode reveals the change.
	saveLikeClaudeCode(t, paths.Credentials, credentialsFor(refreshedToken))

	oauth, err = credentials.Read()
	require.NoError(t, err)
	assert.Equal(t, refreshedToken, oauth.AccessToken.Reveal())

	// The user signs in to another account before Claude Code refetches usage,
	// so the cache belongs to the previous account.
	saveLikeClaudeCode(t, paths.GlobalConfig, globalConfigFor(accountB, accountA))

	state, err = global.Read()
	require.NoError(t, err)
	assert.Equal(t, accountB, state.AccountUUID)

	_, ok = state.UsableCache()
	assert.False(t, ok)

	probe := claudecode.NewActivityProbe(paths.Sessions, time.Minute)
	assert.False(t, probe.Active(flowTime))

	session := filepath.Join(paths.Sessions, "4242.json")
	saveLikeClaudeCode(t, session, `{"status":"idle","pid":4242}`)
	assert.True(t, probe.Active(flowTime.Add(30*time.Second)))
	assert.False(t, probe.Active(flowTime.Add(time.Hour)))
}

// TestConfigDirLayoutFlow reads every file from CLAUDE_CONFIG_DIR, including
// the legacy global config that takes precedence.
func TestConfigDirLayoutFlow(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configDir := t.TempDir()

	legacy := filepath.Join(configDir, ".config.json")
	saveLikeClaudeCode(t, legacy, globalConfigFor(accountB, accountB))

	paths := newFixtureHome(t, configDir, home)

	assert.Equal(t, configDir, paths.Dir)
	assert.Equal(t, legacy, paths.GlobalConfig)
	assert.Equal(t, filepath.Join(configDir, ".credentials.json"), paths.Credentials)

	oauth, err := claudecode.NewCredentialsReader(paths.Credentials).Read()
	require.NoError(t, err)
	assert.Equal(t, firstToken, oauth.AccessToken.Reveal())

	state, err := claudecode.NewGlobalConfigReader(paths.GlobalConfig).Read()
	require.NoError(t, err)
	assert.Equal(t, accountA, state.AccountUUID)

	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written to the home directory")
}

// TestSignedOutFlow reads a home where Claude Code was never set up and then
// follows a login and a logout.
func TestSignedOutFlow(t *testing.T) {
	t.Parallel()

	paths := claudecode.ResolvePaths("", t.TempDir())
	credentials := claudecode.NewCredentialsReader(paths.Credentials)
	global := claudecode.NewGlobalConfigReader(paths.GlobalConfig)

	_, err := credentials.Read()
	require.ErrorIs(t, err, claudecode.ErrNoCredentials)

	state, err := global.Read()
	require.NoError(t, err)
	assert.Equal(t, claudecode.GlobalState{}, state)

	assert.False(t, claudecode.NewActivityProbe(paths.Sessions, time.Minute).Active(flowTime))

	require.NoError(t, os.MkdirAll(paths.Dir, 0o700))
	saveLikeClaudeCode(t, paths.Credentials, credentialsFor(firstToken))

	oauth, err := credentials.Read()
	require.NoError(t, err)
	assert.Equal(t, firstToken, oauth.AccessToken.Reveal())

	saveLikeClaudeCode(t, paths.Credentials, `{"mcpOAuth":{}}`)

	_, err = credentials.Read()
	require.ErrorIs(t, err, claudecode.ErrLoggedOut)
}
