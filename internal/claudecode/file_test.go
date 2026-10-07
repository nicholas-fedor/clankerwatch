// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedModTime is the modification time fixtures are pinned to, so that two
// versions of a file can collide on size and mtime.
var fixedModTime = time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)

// writeFile writes content to path with private permissions and pins its
// modification time to modTime.
//
// Parameters:
//   - t: test handle.
//   - path: the file to write.
//   - content: the file content.
//   - modTime: the modification time to set.
func writeFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

// replaceFile replaces path the way Claude Code does, by writing a temporary
// file next to it and renaming it over the original.
//
// Parameters:
//   - t: test handle.
//   - path: the file to replace.
//   - content: the new content.
//   - modTime: the modification time to set on the new file.
func replaceFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()

	tmp := path + ".tmp"
	writeFile(t, tmp, content, modTime)
	require.NoError(t, os.Rename(tmp, path))
}

// notDirPath returns a path whose parent is a regular file, so stat fails
// with an error other than [fs.ErrNotExist].
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - string: a path below a regular file.
func notDirPath(t *testing.T) string {
	t.Helper()

	parent := filepath.Join(t.TempDir(), "file")
	writeFile(t, parent, "x", fixedModTime)

	return filepath.Join(parent, "child.json")
}

// TestStatIDMissingFile keeps fs.ErrNotExist matchable through the wrap.
func TestStatIDMissingFile(t *testing.T) {
	t.Parallel()

	_, err := statID(filepath.Join(t.TempDir(), "missing.json"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// TestStatIDTracksInode tells two files apart that share size and mtime.
//
// Claude Code renames a temporary file over the original, so only the inode
// distinguishes the new version when the size and timestamp collide.
func TestStatIDTracksInode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.json")
	writeFile(t, path, "aaaa", fixedModTime)

	before, err := statID(path)
	require.NoError(t, err)

	replaceFile(t, path, "bbbb", fixedModTime)

	after, err := statID(path)
	require.NoError(t, err)

	assert.Equal(t, before.size, after.size)
	assert.True(t, before.modTime.Equal(after.modTime))
	assert.NotZero(t, after.inode)
	assert.NotEqual(t, before.inode, after.inode)
	assert.NotEqual(t, before, after)
}

// TestReadLimited covers the size limit and the read failures.
func TestReadLimited(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	writeFile(t, path, "12345", fixedModTime)

	tests := []struct {
		wantErr error
		name    string
		path    string
		want    string
		limit   int64
		failure bool
	}{
		{name: "under the limit", path: path, limit: 10, want: "12345"},
		{name: "at the limit", path: path, limit: 5, want: "12345"},
		{name: "over the limit", path: path, limit: 4, failure: true, wantErr: errTooLarge},
		{name: "missing file", path: filepath.Join(dir, "missing"), limit: 10, failure: true, wantErr: fs.ErrNotExist},
		{name: "directory", path: dir, limit: 10, failure: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := readLimited(tt.path, tt.limit)
			if !tt.failure {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(got))

				return
			}

			require.Error(t, err)
			assert.Nil(t, got)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}

			assert.Contains(t, err.Error(), tt.path)
		})
	}
}
