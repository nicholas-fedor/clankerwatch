// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStateFile returns a File in a directory that does not exist yet.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - File: the store.
func newStateFile(t *testing.T) File {
	t.Helper()

	return File{Path: filepath.Join(t.TempDir(), "clankerwatch", "state.json")}
}

// dirEntries lists the names in dir.
//
// Parameters:
//   - t: test handle.
//   - dir: the directory to list.
//
// Returns:
//   - []string: the entry names.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// TestFileLoadMissing returns a fresh state without an error.
func TestFileLoadMissing(t *testing.T) {
	t.Parallel()

	got, err := newStateFile(t).Load()
	require.NoError(t, err)
	assert.Equal(t, Fresh(), got)
}

// TestFileLoadDiscards returns a fresh state and the reason for every file
// that cannot be used.
func TestFileLoadDiscards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		content string
		want    string
	}{
		{name: "truncated", content: `{"version":1,"data":`, want: "discarding unreadable state"},
		{name: "wrong type", content: `{"version":"1"}`, want: "discarding unreadable state"},
		{name: "older version", content: `{"version":0}`, wantErr: ErrVersion, want: "unsupported state version 0"},
		{name: "newer version", content: `{"version":2}`, wantErr: ErrVersion, want: "unsupported state version 2"},
		{name: "no version", content: `{}`, wantErr: ErrVersion},
		{name: "null", content: `null`, wantErr: ErrVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "state.json")
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))

			got, err := File{Path: path}.Load()
			require.Error(t, err)
			assert.Equal(t, Fresh(), got)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}

			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestFileLoadReadError reports a path that cannot be read as a file.
func TestFileLoadReadError(t *testing.T) {
	t.Parallel()

	got, err := File{Path: t.TempDir()}.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read state")
	assert.Equal(t, Fresh(), got)
}

// TestFileRoundTrip loads exactly what was saved.
func TestFileRoundTrip(t *testing.T) {
	t.Parallel()

	file := newStateFile(t)
	require.NoError(t, file.Save(sampleState()))

	got, err := file.Load()
	require.NoError(t, err)
	assertSameState(t, sampleState(), got)
}

// TestFileSavePermissions keeps the state file and its directory private.
func TestFileSavePermissions(t *testing.T) {
	t.Parallel()

	file := newStateFile(t)
	require.NoError(t, file.Save(sampleState()))

	info, err := os.Stat(file.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Dir(file.Path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
}

// TestFileSaveReplacesAtomically swaps in a new file and leaves no temporary
// file behind.
func TestFileSaveReplacesAtomically(t *testing.T) {
	t.Parallel()

	file := newStateFile(t)
	require.NoError(t, file.Save(sampleState()))

	before, err := os.Stat(file.Path)
	require.NoError(t, err)

	require.NoError(t, file.Save(Fresh()))

	after, err := os.Stat(file.Path)
	require.NoError(t, err)
	assert.False(t, os.SameFile(before, after), "the file is replaced, not rewritten in place")

	content, err := os.ReadFile(file.Path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1}`, string(content))
	assert.Equal(t, byte('\n'), content[len(content)-1])

	assert.Equal(t, []string{"state.json"}, dirEntries(t, filepath.Dir(file.Path)))
}

// TestFileSaveNeverStoresTheToken keeps credentials out of the file.
func TestFileSaveNeverStoresTheToken(t *testing.T) {
	t.Parallel()

	file := newStateFile(t)
	require.NoError(t, file.Save(sampleState()))

	content, err := os.ReadFile(file.Path)
	require.NoError(t, err)

	var fields map[string]json.RawMessage

	require.NoError(t, json.Unmarshal(content, &fields))
	assert.NotContains(t, fields, "token")
	assert.NotContains(t, fields, "accessToken")
}

// TestFileSaveDirError reports a directory that cannot be created.
func TestFileSaveDirError(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(parent, nil, 0o600))

	err := File{Path: filepath.Join(parent, "state.json")}.Save(Fresh())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create state dir")
}

// TestFileSaveEncodeError leaves the previous file untouched when the state
// cannot be encoded.
func TestFileSaveEncodeError(t *testing.T) {
	t.Parallel()

	file := newStateFile(t)
	require.NoError(t, file.Save(sampleState()))

	broken := Fresh()
	broken.Data = &Data{Payload: json.RawMessage(`{`)}

	err := file.Save(broken)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encode state")

	got, err := file.Load()
	require.NoError(t, err)
	assertSameState(t, sampleState(), got)
}

// TestFileSaveCreateTempError reports a directory that refuses new files.
func TestFileSaveCreateTempError(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := File{Path: filepath.Join(dir, "state.json")}.Save(Fresh())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create state file")
}

// TestFileSaveRenameError removes the temporary file when the rename fails.
func TestFileSaveRenameError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	require.NoError(t, os.Mkdir(target, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(target, "keep"), nil, 0o600))

	err := File{Path: target}.Save(Fresh())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace state file")

	assert.Equal(t, []string{"state.json"}, dirEntries(t, dir))
}

// TestWriteAndCloseError reports a file that can no longer be written.
func TestWriteAndCloseError(t *testing.T) {
	t.Parallel()

	tmp, err := os.CreateTemp(t.TempDir(), "state-*")
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	err = writeAndClose(tmp, []byte("{}"))
	require.ErrorIs(t, err, os.ErrClosed)
	assert.Contains(t, err.Error(), "write state file")
}
