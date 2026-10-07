// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FuzzFileLoad feeds arbitrary bytes to the state file loader.
//
// No input may panic, every result must be in the current format, and a
// discarded file must come back as a fresh state. The seeds cover a full
// state, a fresh state, every version mismatch, type confusion in nested
// fields, a payload that is not an object, and truncated JSON.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzFileLoad(f *testing.F) {
	full, err := json.Marshal(sampleState())
	if err != nil {
		f.Fatal(err)
	}

	f.Add(full)
	f.Add([]byte(`{"version":1}`))
	f.Add([]byte(`{"version":0}`))
	f.Add([]byte(`{"version":2,"data":{"payload":{}}}`))
	f.Add([]byte(`{"version":1,"alerts":{"entries":[{"bar":1}]}}`))
	f.Add([]byte(`{"version":1,"data":{"payload":"text"}}`))
	f.Add([]byte(`{"version":1,"backoff":{"until":"yesterday"}}`))
	f.Add(full[:len(full)/2])
	f.Add([]byte(`null`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, content []byte) {
		path := filepath.Join(t.TempDir(), "state.json")

		err := os.WriteFile(path, content, 0o600)
		if err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		loaded, err := File{Path: path}.Load()
		if loaded.Version != Version {
			t.Fatalf("loaded version %d, want %d (error %v)", loaded.Version, Version, err)
		}

		if err != nil {
			if loaded.Data != nil || loaded.AuthBlock != nil || len(loaded.Alerts.Entries) != 0 {
				t.Fatalf("discarded file returned data: %+v", loaded)
			}

			return
		}

		// A loaded state must survive the daemon's next save.
		_, err = clone(loaded)
		if err != nil {
			t.Fatalf("loaded state cannot be saved again: %v", err)
		}
	})
}
