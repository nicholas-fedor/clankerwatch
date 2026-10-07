// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fuzzToken is the access token planted in credentials seeds. Error text must
// never repeat it, whatever surrounds it in the file.
const fuzzToken = "sk-ant-oat01-FUZZ-SENTINEL-TOKEN"

// writeFuzzFile writes content to a new file in a fresh temporary directory.
//
// Parameters:
//   - t: test handle.
//   - name: the file name.
//   - content: the file content.
//
// Returns:
//   - string: the file path.
func writeFuzzFile(t *testing.T, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	err := os.WriteFile(path, content, 0o600)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	return path
}

// FuzzReadCredentials feeds arbitrary bytes to the credentials reader.
//
// No input may panic, error text must never carry the planted token, and a
// successful read must yield a token and an expiry. The seeds cover a full
// login, each logged-out shape, type confusion around the token, a number too
// large for float64, and truncated JSON that ends inside the token.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzReadCredentials(f *testing.F) {
	f.Add([]byte(credentialsJSON(fuzzToken, testExpiresMs)))
	f.Add([]byte(`{"claudeAiOauth":{"accessToken":"` + fuzzToken + `","expiresAt":1}}`))
	f.Add([]byte(`{"claudeAiOauth":{"accessToken":"` + fuzzToken + `"}}`))
	f.Add([]byte(`{"claudeAiOauth":{"accessToken":"` + fuzzToken + `","expiresAt":"soon"}}`))
	f.Add([]byte(`{"claudeAiOauth":{"accessToken":"` + fuzzToken + `","expiresAt":1e999}}`))
	f.Add([]byte(`{"claudeAiOauth":{"accessToken":["` + fuzzToken + `"],"expiresAt":1}}`))
	f.Add([]byte(`{"claudeAiOauth":{"accessToken":"` + fuzzToken[:10]))
	f.Add([]byte(`{"claudeAiOauth":null}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
	f.Add([]byte("\xff\xfe"))

	f.Fuzz(func(t *testing.T, content []byte) {
		path := writeFuzzFile(t, credentialsName, content)

		oauth, err := readCredentials(path)
		if err != nil {
			if strings.Contains(err.Error(), fuzzToken) {
				t.Fatalf("error leaks the token: %v", err)
			}

			if oauth.AccessToken != "" || !oauth.ExpiresAt.IsZero() {
				t.Fatalf("failed read returned a login: %+v", oauth)
			}

			return
		}

		if oauth.AccessToken == "" {
			t.Fatal("successful read without a token")
		}

		if oauth.ExpiresAt.IsZero() {
			t.Fatal("successful read without an expiry")
		}

		if rendered := fmt.Sprint(oauth.AccessToken); rendered != redacted {
			t.Fatalf("token renders as %q", rendered)
		}

		reader := NewCredentialsReader(path)

		again, err := reader.Read()
		if err != nil || again.AccessToken != oauth.AccessToken {
			t.Fatalf("reader disagrees with readCredentials: %v", err)
		}
	})
}

// FuzzGlobalConfig feeds arbitrary bytes to the global config reader.
//
// No input may panic, and UsableCache may only return a cache whose account
// matches the signed-in account. The seeds cover a matching cache, a cache
// for another account, a null and a missing payload, type confusion, and a
// truncated file.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzGlobalConfig(f *testing.F) {
	f.Add([]byte(globalConfigJSON(testAccount, testAccount)))
	f.Add([]byte(globalConfigJSON(testAccount, otherAccount)))
	f.Add([]byte(globalConfigJSON("", "")))
	f.Add([]byte(`{"cachedUsageUtilization":{"accountUuid":"a","fetchedAtMs":1,"utilization":null}}`))
	f.Add([]byte(`{"cachedUsageUtilization":{"accountUuid":"a","fetchedAtMs":1}}`))
	f.Add([]byte(`{"oauthAccount":"a","cachedUsageUtilization":[]}`))
	f.Add([]byte(`{"oauthAccount":{"accountUuid":"a"},"cachedUsageUtilization":{"accountUuid":"a","fetchedAtMs":-1,"utilization":{}}}`))
	f.Add([]byte(`{"oauthAccount":{"accountUu`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, content []byte) {
		path := writeFuzzFile(t, globalConfigName, content)

		state, err := readGlobalConfig(path)
		if err != nil {
			if state.AccountUUID != "" || state.Cached != nil {
				t.Fatalf("failed read returned state: %+v", state)
			}
		}

		if state.Cached != nil && (state.Cached.FetchedAt.IsZero() || len(state.Cached.Payload) == 0) {
			t.Fatalf("cache without a fetch time or payload: %+v", state.Cached)
		}

		cache, ok := state.UsableCache()
		if ok && (cache.AccountUUID == "" || cache.AccountUUID != state.AccountUUID) {
			t.Fatalf("usable cache for %q while %q is signed in", cache.AccountUUID, state.AccountUUID)
		}

		if !ok && cache.Payload != nil {
			t.Fatal("unusable cache returned a payload")
		}

		reader := NewGlobalConfigReader(path)

		viaReader, readErr := reader.Read()
		if (readErr == nil) != (err == nil) {
			t.Fatalf("reader error %v, readGlobalConfig error %v", readErr, err)
		}

		if viaReader.AccountUUID != state.AccountUUID {
			t.Fatalf("reader account %q, want %q", viaReader.AccountUUID, state.AccountUUID)
		}
	})
}
