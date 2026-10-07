// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// secretValue is the credential the redaction tests try to leak.
	secretValue = "sk-ant-oat01-never-print-me"

	// logKeyToken is the log attribute key for a bare secret.
	logKeyToken = "token"

	// logKeyOAuth is the log attribute key for a login holding a secret.
	logKeyOAuth = "oauth"
)

// TestSecretFormatVerbs redacts the secret under every fmt verb, both bare and
// inside a struct.
func TestSecretFormatVerbs(t *testing.T) {
	t.Parallel()

	secret := Secret(secretValue)
	oauth := OAuth{AccessToken: secret, SubscriptionType: "max"}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%10.3s"} {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()

			bare := fmt.Sprintf(verb, secret)
			assert.NotContains(t, bare, secretValue)
			assert.Contains(t, bare, redacted)

			nested := fmt.Sprintf(verb, oauth)
			assert.NotContains(t, nested, secretValue)
			assert.NotContains(t, nested, fmt.Sprintf("%x", secretValue))
		})
	}
}

// TestSecretStringers redacts through the plain string conversions.
func TestSecretStringers(t *testing.T) {
	t.Parallel()

	secret := Secret(secretValue)

	assert.Equal(t, redacted, secret.String())
	assert.Equal(t, redacted, secret.GoString())
	assert.Equal(t, redacted, fmt.Sprint(secret))
	assert.Equal(t, redacted, fmt.Sprint(&secret))
	assert.Equal(t, redacted+"\n", fmt.Sprintln(secret))
	assert.Equal(t, secretValue, secret.Reveal())
}

// TestSecretMarshal redacts JSON and text encodings, bare and nested.
func TestSecretMarshal(t *testing.T) {
	t.Parallel()

	secret := Secret(secretValue)

	encoded, err := json.Marshal(secret)
	require.NoError(t, err)
	assert.JSONEq(t, `"[redacted]"`, string(encoded))

	encoded, err = json.Marshal(struct {
		Token Secret `json:"token"`
	}{Token: secret})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), secretValue)
	assert.Contains(t, string(encoded), redacted)

	encoded, err = json.Marshal(map[Secret]Secret{secret: secret})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), secretValue)

	text, err := secret.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, redacted, string(text))
}

// TestSecretSlog redacts the secret in text and JSON log records.
func TestSecretSlog(t *testing.T) {
	t.Parallel()

	secret := Secret(secretValue)

	for _, tt := range []struct {
		handler func(buf *bytes.Buffer) slog.Handler
		name    string
	}{
		{name: "json", handler: func(buf *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buf, nil) }},
		{name: "text", handler: func(buf *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buf, nil) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := slog.New(tt.handler(&buf))
			logger.InfoContext(t.Context(), "Bare secret",
				slog.Any(logKeyToken, secret),
			)
			logger.InfoContext(t.Context(), "Nested secret",
				slog.Any(logKeyOAuth, OAuth{AccessToken: secret}),
			)
			logger.InfoContext(t.Context(), "Grouped secret",
				slog.Group(logKeyOAuth, slog.Any(logKeyToken, secret)),
			)

			assert.NotContains(t, buf.String(), secretValue)
			assert.Contains(t, buf.String(), redacted)
		})
	}
}
