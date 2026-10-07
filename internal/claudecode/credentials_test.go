// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture values for a signed-in credentials file.
const (
	// testToken is the access token in credentials fixtures.
	testToken = "sk-ant-oat01-test-access-token"

	// testRefreshToken is the refresh credential, which must never be decoded.
	testRefreshToken = "sk-ant-ort01-test-refresh-token"

	// testExpiresMs is the access token expiry in Unix milliseconds.
	testExpiresMs int64 = 1_784_000_000_000

	// testLoginExpiresMs is the refresh credential expiry in Unix milliseconds.
	testLoginExpiresMs int64 = 1_790_000_000_000
)

// credentialsJSON renders a credentials file the way Claude Code writes it.
//
// Parameters:
//   - token: the access token.
//   - expiresMs: the access token expiry in Unix milliseconds.
//
// Returns:
//   - string: the file content, including fields the reader must skip.
func credentialsJSON(token string, expiresMs int64) string {
	return fmt.Sprintf(`{
  "claudeAiOauth": {
    "accessToken": %q,
    "refreshToken": %q,
    "expiresAt": %d,
    "refreshTokenExpiresAt": %d,
    "scopes": ["user:inference", "user:profile"],
    "subscriptionType": "max",
    "rateLimitTier": "default_claude_max_5x"
  },
  "mcpOAuth": {"server": {"accessToken": "mcp-token"}}
}`, token, testRefreshToken, expiresMs, testLoginExpiresMs)
}

// newCredentialsFile writes a signed-in credentials file into a new temporary
// directory.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - string: the credentials file path.
func newCredentialsFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), credentialsName)
	writeFile(t, path, credentialsJSON(testToken, testExpiresMs), fixedModTime)

	return path
}

// TestReadCredentials covers the decoded fields and the logged-out shapes.
func TestReadCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		content string
		want    OAuth
		failure bool
	}{
		{
			name:    "signed in",
			content: credentialsJSON(testToken, testExpiresMs),
			want: OAuth{
				ExpiresAt:        time.UnixMilli(testExpiresMs),
				LoginExpiresAt:   time.UnixMilli(testLoginExpiresMs),
				AccessToken:      Secret(testToken),
				SubscriptionType: "max",
				RateLimitTier:    "default_claude_max_5x",
				Scopes:           []string{"user:inference", "user:profile"},
			},
		},
		{
			name:    "no login expiry",
			content: `{"claudeAiOauth":{"accessToken":"tok","expiresAt":1000}}`,
			want:    OAuth{AccessToken: "tok", ExpiresAt: time.UnixMilli(1000)},
		},
		{name: "no subscription login", content: `{"mcpOAuth":{}}`, failure: true, wantErr: ErrLoggedOut},
		{name: "null subscription login", content: `{"claudeAiOauth":null}`, failure: true, wantErr: ErrLoggedOut},
		{
			name:    "empty token",
			content: `{"claudeAiOauth":{"accessToken":"","expiresAt":1000}}`,
			failure: true,
			wantErr: ErrLoggedOut,
		},
		{
			name:    "no expiry",
			content: `{"claudeAiOauth":{"accessToken":"tok"}}`,
			failure: true,
			wantErr: ErrLoggedOut,
		},
		{name: "invalid json", content: `{"claudeAiOauth":`, failure: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), credentialsName)
			writeFile(t, path, tt.content, fixedModTime)

			got, err := readCredentials(path)
			if tt.failure {
				require.Error(t, err)
				assert.Equal(t, OAuth{}, got)

				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestReadCredentialsTooLarge rejects a file over the size limit.
func TestReadCredentialsTooLarge(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), credentialsName)
	writeFile(t, path, strings.Repeat(" ", credentialsLimit+1), fixedModTime)

	_, err := readCredentials(path)
	require.ErrorIs(t, err, errTooLarge)
}

// TestReadCredentialsSkipsRefreshToken keeps the refresh credential out of
// everything the reader returns.
func TestReadCredentialsSkipsRefreshToken(t *testing.T) {
	t.Parallel()

	got, err := readCredentials(newCredentialsFile(t))
	require.NoError(t, err)

	assert.NotContains(t, fmt.Sprintf("%+v", got), testRefreshToken)
	assert.NotContains(t, got.AccessToken.Reveal(), testRefreshToken)
}

// TestCredentialsReaderMissingFile reports ErrNoCredentials.
func TestCredentialsReaderMissingFile(t *testing.T) {
	t.Parallel()

	reader := NewCredentialsReader(filepath.Join(t.TempDir(), credentialsName))

	got, err := reader.Read()
	require.ErrorIs(t, err, ErrNoCredentials)
	assert.Equal(t, OAuth{}, got)
}

// TestCredentialsReaderStatError wraps a stat failure that is not a missing
// file.
func TestCredentialsReaderStatError(t *testing.T) {
	t.Parallel()

	reader := NewCredentialsReader(notDirPath(t))

	_, err := reader.Read()
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoCredentials)
	assert.Contains(t, err.Error(), "check credentials")
}

// TestCredentialsReaderSkipsUnchangedFile returns the cached login while the
// file identity is unchanged, even when the bytes differ.
func TestCredentialsReaderSkipsUnchangedFile(t *testing.T) {
	t.Parallel()

	path := newCredentialsFile(t)
	reader := NewCredentialsReader(path)

	first, err := reader.Read()
	require.NoError(t, err)

	// Same length, same inode, same mtime: the reader cannot tell, and must not
	// spend a read on it.
	swapped := strings.Replace(credentialsJSON(testToken, testExpiresMs), "test-access", "TEST-ACCESS", 1)
	writeFile(t, path, swapped, fixedModTime)

	second, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, testToken, second.AccessToken.Reveal())
}

// TestCredentialsReaderRereadsAfterRename picks up a file renamed over the
// original even when size and mtime are identical.
func TestCredentialsReaderRereadsAfterRename(t *testing.T) {
	t.Parallel()

	path := newCredentialsFile(t)
	reader := NewCredentialsReader(path)

	first, err := reader.Read()
	require.NoError(t, err)
	require.Equal(t, testToken, first.AccessToken.Reveal())

	const renewed = "sk-ant-oat01-RENEWED-access-to"

	require.Len(t, renewed, len(testToken))
	replaceFile(t, path, credentialsJSON(renewed, testExpiresMs), fixedModTime)

	second, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, renewed, second.AccessToken.Reveal())
}

// TestCredentialsReaderCachesErrors returns the cached error for an unchanged
// file and recovers once the file changes or disappears.
func TestCredentialsReaderCachesErrors(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), credentialsName)
	writeFile(t, path, `{}`, fixedModTime)

	reader := NewCredentialsReader(path)

	_, err := reader.Read()
	require.ErrorIs(t, err, ErrLoggedOut)

	_, err = reader.Read()
	require.ErrorIs(t, err, ErrLoggedOut)

	require.NoError(t, os.Remove(path))

	_, err = reader.Read()
	require.ErrorIs(t, err, ErrNoCredentials)

	writeFile(t, path, credentialsJSON(testToken, testExpiresMs), fixedModTime)

	got, err := reader.Read()
	require.NoError(t, err)
	assert.Equal(t, testToken, got.AccessToken.Reveal())
}

// TestCredentialsReaderNeverModifiesFile leaves content, mode, and mtime
// untouched across reads.
func TestCredentialsReaderNeverModifiesFile(t *testing.T) {
	t.Parallel()

	path := newCredentialsFile(t)

	before, err := os.Stat(path)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	reader := NewCredentialsReader(path)

	for range 3 {
		_, err = reader.Read()
		require.NoError(t, err)
	}

	_, err = readCredentials(path)
	require.NoError(t, err)

	after, err := os.Stat(path)
	require.NoError(t, err)

	again, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, content, again)
	assert.Equal(t, before.Mode(), after.Mode())
	assert.True(t, before.ModTime().Equal(after.ModTime()))
	assert.Equal(t, before.Size(), after.Size())
}

// TestOAuthHasScope matches scopes exactly.
func TestOAuthHasScope(t *testing.T) {
	t.Parallel()

	oauth := OAuth{Scopes: []string{"user:inference", "user:profile"}}

	assert.True(t, oauth.HasScope("user:profile"))
	assert.False(t, oauth.HasScope("user"))
	assert.False(t, OAuth{}.HasScope("user:profile"))
}

// TestOAuthUsableAt requires a token that outlives the skew.
func TestOAuthUsableAt(t *testing.T) {
	t.Parallel()

	now := fixedModTime
	expires := now.Add(10 * time.Minute)

	tests := []struct {
		name  string
		oauth OAuth
		skew  time.Duration
		want  bool
	}{
		{name: "well before expiry", oauth: OAuth{AccessToken: "tok", ExpiresAt: expires}, skew: time.Minute, want: true},
		{name: "inside the skew", oauth: OAuth{AccessToken: "tok", ExpiresAt: expires}, skew: 10 * time.Minute},
		{name: "expired", oauth: OAuth{AccessToken: "tok", ExpiresAt: now.Add(-time.Second)}},
		{name: "no token", oauth: OAuth{ExpiresAt: expires}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.oauth.UsableAt(now, tt.skew))
		})
	}
}
