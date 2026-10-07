// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"
)

// OAuth is the subset of Claude Code's subscription login the daemon needs.
type OAuth struct {
	// ExpiresAt is when the access token expires.
	ExpiresAt time.Time

	// LoginExpiresAt is when the whole login lapses and /login is needed.
	LoginExpiresAt time.Time

	// AccessToken is the bearer token for the usage endpoint.
	AccessToken Secret

	// SubscriptionType is the plan, such as max or pro.
	SubscriptionType string

	// RateLimitTier is the plan's tier, such as default_claude_max_5x.
	RateLimitTier string

	// Scopes are the OAuth scopes granted to the token.
	Scopes []string
}

// CredentialsReader reads Claude Code's credentials file and re-reads it only
// after it changes.
type CredentialsReader struct {
	err   error
	oauth OAuth
	path  string
	id    fileID
	read  bool
}

// credentialsFile lists the only fields the daemon decodes.
//
// Everything else in the file, including the refresh credential and MCP
// logins, is skipped by the decoder.
type credentialsFile struct {
	ClaudeAiOauth *subscriptionLogin `json:"claudeAiOauth"`
}

// subscriptionLogin is the claudeAiOauth object of the credentials file.
type subscriptionLogin struct {
	AccessToken           string   `json:"accessToken"`
	SubscriptionType      string   `json:"subscriptionType"`
	RateLimitTier         string   `json:"rateLimitTier"`
	Scopes                []string `json:"scopes"`
	ExpiresAt             float64  `json:"expiresAt"`
	RefreshTokenExpiresAt float64  `json:"refreshTokenExpiresAt"`
}

// credentialsLimit is the largest credentials file the reader accepts.
const credentialsLimit = 1 << 20

var (
	// ErrNoCredentials indicates that Claude Code has no credentials file.
	ErrNoCredentials = errors.New("claude code credentials not found")

	// ErrLoggedOut indicates a credentials file without a usable subscription login.
	ErrLoggedOut = errors.New("claude code is logged out")
)

// NewCredentialsReader returns a reader for the credentials file at path.
//
// Parameters:
//   - path: the credentials file.
//
// Returns:
//   - *CredentialsReader: a reader that has not read the file yet.
func NewCredentialsReader(path string) *CredentialsReader {
	return &CredentialsReader{path: path, id: fileID{}, read: false, oauth: OAuth{}, err: nil}
}

// Read returns the current login.
//
// The file is read again only when its identity changed since the last call,
// and the previous result, including its error, is returned otherwise.
//
// Returns:
//   - OAuth: the login.
//   - error: ErrNoCredentials when the file is missing, ErrLoggedOut when it
//     holds no subscription login, or a read or parse error.
func (r *CredentialsReader) Read() (OAuth, error) {
	id, err := statID(r.path)
	if err != nil {
		r.read = false

		if errors.Is(err, fs.ErrNotExist) {
			return OAuth{}, ErrNoCredentials
		}

		return OAuth{}, fmt.Errorf("check credentials: %w", err)
	}

	if r.read && id == r.id {
		return r.oauth, r.err
	}

	r.oauth, r.err = readCredentials(r.path)
	r.id, r.read = id, true

	return r.oauth, r.err
}

// HasScope reports whether the token was granted scope.
//
// Parameters:
//   - scope: the OAuth scope, such as user:profile.
//
// Returns:
//   - bool: true when the scope was granted.
func (o OAuth) HasScope(scope string) bool { return slices.Contains(o.Scopes, scope) }

// UsableAt reports whether the token stays valid for at least skew after now.
//
// Claude Code refreshes tokens shortly before they expire, so a token inside
// that window is left alone.
//
// Parameters:
//   - now: the current time.
//   - skew: how long the token must stay valid.
//
// Returns:
//   - bool: true when the token can be used.
func (o OAuth) UsableAt(now time.Time, skew time.Duration) bool {
	return o.AccessToken != "" && now.Add(skew).Before(o.ExpiresAt)
}

// readCredentials parses the credentials file.
//
// Parameters:
//   - path: the credentials file.
//
// Returns:
//   - OAuth: the login.
//   - error: ErrLoggedOut without a usable login, or a read or parse error.
func readCredentials(path string) (OAuth, error) {
	content, err := readLimited(path, credentialsLimit)
	if err != nil {
		return OAuth{}, fmt.Errorf("read credentials: %w", err)
	}

	defer clear(content)

	var file credentialsFile

	err = json.Unmarshal(content, &file)
	if err != nil {
		return OAuth{}, fmt.Errorf("parse credentials: %w", err)
	}

	login := file.ClaudeAiOauth
	if login == nil || login.AccessToken == "" || login.ExpiresAt <= 0 {
		return OAuth{}, ErrLoggedOut
	}

	oauth := OAuth{
		AccessToken:      Secret(login.AccessToken),
		ExpiresAt:        time.UnixMilli(int64(login.ExpiresAt)),
		LoginExpiresAt:   time.Time{},
		Scopes:           login.Scopes,
		SubscriptionType: login.SubscriptionType,
		RateLimitTier:    login.RateLimitTier,
	}
	if login.RefreshTokenExpiresAt > 0 {
		oauth.LoginExpiresAt = time.UnixMilli(int64(login.RefreshTokenExpiresAt))
	}

	return oauth, nil
}
