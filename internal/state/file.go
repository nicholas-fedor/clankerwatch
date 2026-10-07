// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File keeps the state in a JSON file.
type File struct {
	// Path is the state file.
	Path string
}

const (
	// dirMode keeps the state directory private.
	dirMode = 0o700

	// fileMode keeps the state file private.
	fileMode = 0o600
)

// ErrVersion indicates a state file written in another format.
var ErrVersion = errors.New("unsupported state version")

// Load reads the state file.
//
// Returns:
//   - State: the saved state, or a fresh state when the file is missing,
//     unreadable, or in another format.
//   - error: why a present file was discarded.
func (f File) Load() (State, error) {
	fresh := Fresh()

	content, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return fresh, nil
	}

	if err != nil {
		return fresh, fmt.Errorf("read state: %w", err)
	}

	var loaded State

	err = json.Unmarshal(content, &loaded)
	if err != nil {
		return fresh, fmt.Errorf("discarding unreadable state: %w", err)
	}

	if loaded.Version != Version {
		return fresh, fmt.Errorf("%w %d", ErrVersion, loaded.Version)
	}

	return loaded, nil
}

// Save writes the state through a temporary file and a rename.
//
// Parameters:
//   - current: the state to save.
//
// Returns:
//   - error: a create, write, or rename error.
func (f File) Save(current State) error {
	dir := filepath.Dir(f.Path)

	err := os.MkdirAll(dir, dirMode)
	if err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	content, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create state file: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	err = writeAndClose(tmp, append(content, '\n'))
	if err != nil {
		return err
	}

	err = os.Rename(tmpPath, f.Path)
	if err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}

	return nil
}

// writeAndClose writes content with private permissions, syncs, and closes.
//
// Parameters:
//   - tmp: the open temporary file.
//   - content: the bytes to write.
//
// Returns:
//   - error: the first failure.
func writeAndClose(tmp *os.File, content []byte) error {
	err := tmp.Chmod(fileMode)
	if err == nil {
		_, err = tmp.Write(content)
	}

	if err == nil {
		err = tmp.Sync()
	}

	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}

	if err != nil {
		return fmt.Errorf("write state file: %w", err)
	}

	return nil
}
