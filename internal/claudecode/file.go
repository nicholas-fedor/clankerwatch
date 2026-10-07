// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

// fileID identifies one version of a file well enough to skip re-reading it
// while it is unchanged.
//
// Claude Code replaces files by renaming a temporary file over them, so the
// inode changes even when size and modification time collide.
type fileID struct {
	modTime time.Time
	size    int64
	inode   uint64
}

// errTooLarge indicates a file larger than the reader accepts.
var errTooLarge = errors.New("file exceeds the size limit")

// statID returns the identity of the file at path.
//
// Parameters:
//   - path: the file to stat.
//
// Returns:
//   - fileID: modification time, size, and inode.
//   - error: the wrapped stat error, which still matches [fs.ErrNotExist].
func statID(path string) (fileID, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileID{}, fmt.Errorf("stat %s: %w", path, err)
	}

	id := fileID{modTime: info.ModTime(), size: info.Size(), inode: 0}

	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		id.inode = st.Ino
	}

	return id, nil
}

// readLimited reads a whole file opened read-only.
//
// Parameters:
//   - path: the file to read.
//   - limit: the largest accepted size in bytes.
//
// Returns:
//   - []byte: the file content.
//   - error: errTooLarge for a larger file, or the read error.
func readLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if int64(len(content)) > limit {
		clear(content)

		return nil, fmt.Errorf("%s: %w", path, errTooLarge)
	}

	return content, nil
}
