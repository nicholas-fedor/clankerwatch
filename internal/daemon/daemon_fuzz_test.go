// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzDecodePatch checks the SetSettings decoder on arbitrary input.
//
// The argument comes from any process on the session bus, so decoding must
// never panic, and every failure must wrap ErrInvalidPatch so the widget can
// tell a bad change from a failed write. Input that decodes must be a single
// valid JSON document, and re-encoding the patch then decoding it again must
// give the same patch and the same bytes. The seeds cover every field, null,
// an empty object, unknown fields, wrong types, trailing data, stray closing
// brackets, and invalid UTF-8.
func FuzzDecodePatch(f *testing.F) {
	f.Add(`{}`)
	f.Add(`null`)
	f.Add(`{"mode":"cache-only","intervalSeconds":300,"idleIntervalSeconds":1200,"notify":[80,95],"notifyAuth":true}`)
	f.Add(`{"notify":[]}`)
	f.Add(`{"notify":null}`)
	f.Add(`{"intervalSeconds":-1}`)
	f.Add(`{"intervalSeconds":1e3}`)
	f.Add(`{"bogus":1}`)
	f.Add(`{"mode":1}`)
	f.Add(`{} {}`)
	f.Add(`{}]`)
	f.Add(`{"mode":"é<>"}`)
	f.Add("{\"mode\":\"\xff\"}")
	f.Add(``)

	f.Fuzz(func(t *testing.T, input string) {
		var (
			patch settingsPatch
			err   error
		)

		require.NotPanics(t, func() { patch, err = decodePatch(input) })

		if err != nil {
			require.ErrorIs(t, err, ErrInvalidPatch)
			assert.Equal(t, settingsPatch{}, patch)

			return
		}

		assert.True(t, json.Valid([]byte(input)), "accepted input %q is not one JSON document", input)

		encoded, err := json.Marshal(patch)
		require.NoError(t, err)

		if len(encoded) > maxPatchSize {
			// Escaping can grow a patch past the size cap, which must then
			// be refused like any other oversized input.
			_, err = decodePatch(string(encoded))
			require.ErrorIs(t, err, ErrInvalidPatch)

			return
		}

		again, err := decodePatch(string(encoded))
		require.NoError(t, err, "re-encoded patch %s", encoded)
		assert.Equal(t, patch, again)

		reencoded, err := json.Marshal(again)
		require.NoError(t, err)
		assert.Equal(t, string(encoded), string(reencoded))
	})
}
