// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"fmt"
	"io"
	"log/slog"
)

// Secret holds a credential and keeps it out of logs, formatted output, and
// JSON. Only Reveal returns the value.
//
// Secret is a struct rather than a string type. [encoding/json] writes map
// keys of string kind verbatim without calling MarshalText, so a string-based
// Secret used as a map key would leak. A struct also rules out converting a
// Secret to a string to get around Reveal.
type Secret struct {
	// value is the credential.
	value string
}

const (
	// redacted replaces a Secret in every rendering.
	redacted = "[redacted]"

	// redactedJSON is redacted encoded as a JSON string.
	redactedJSON = `"` + redacted + `"`
)

// NewSecret wraps a credential in a Secret.
//
// Parameters:
//   - value: the credential.
//
// Returns:
//   - Secret: the credential, redacted in every rendering.
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// Format writes the redaction marker for every fmt verb.
//
// Parameters:
//   - state: the fmt state to write to.
//   - verb: the formatting verb, which is ignored.
func (Secret) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, redacted) //nolint:errcheck // fmt.Formatter cannot report write errors.
}

// GoString returns the redaction marker for the %#v verb.
//
// Returns:
//   - string: the redaction marker.
func (Secret) GoString() string { return redacted }

// IsZero reports whether the Secret holds no credential.
//
// Returns:
//   - bool: true when the credential is empty.
func (s Secret) IsZero() bool { return s.value == "" }

// LogValue returns the redaction marker for [log/slog].
//
// Returns:
//   - [slog.Value]: the redaction marker.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON encodes the redaction marker.
//
// Returns:
//   - []byte: the JSON-encoded redaction marker.
//   - error: always nil.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(redactedJSON), nil }

// MarshalText encodes the redaction marker.
//
// Returns:
//   - []byte: the redaction marker.
//   - error: always nil.
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Reveal returns the credential. Use it only to build a request header.
//
// Returns:
//   - string: the credential.
func (s Secret) Reveal() string { return s.value }

// String returns the redaction marker.
//
// Returns:
//   - string: the redaction marker.
func (Secret) String() string { return redacted }
