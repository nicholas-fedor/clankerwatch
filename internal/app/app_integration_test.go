// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package app_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/app"
)

// TestRunExitCodes checks the process entry point end to end.
//
// Run reads os.Args and writes to os.Stdout and os.Stderr, so the test swaps
// all three. None of the commands used here touches the bus or ~/.claude.
func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		stderr string
		args   []string
		code   int
	}{
		{name: "version", args: []string{"version"}, code: 0, stdout: "clankerwatch "},
		{name: "invalid setting", args: []string{"status", "--interval", "1s"}, code: 2, stderr: "invalid setting"},
		{name: "unknown command", args: []string{"srve"}, code: 1, stderr: "unknown command"},
		{name: "stray argument", args: []string{"version", "extra"}, code: 1, stderr: "clankerwatch: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runProcess(t, tt.args...)

			assert.Equal(t, tt.code, code)
			assert.Contains(t, stdout, tt.stdout)
			assert.Contains(t, stderr, tt.stderr)
		})
	}
}

// runProcess calls app.Run as the process would and returns its output.
func runProcess(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	dir := t.TempDir()
	stdoutPath, stderrPath := filepath.Join(dir, "stdout"), filepath.Join(dir, "stderr")

	stdout, err := os.Create(stdoutPath)
	require.NoError(t, err)

	stderr, err := os.Create(stderrPath)
	require.NoError(t, err)

	arguments, realStdout, realStderr := os.Args, os.Stdout, os.Stderr

	t.Cleanup(func() { os.Args, os.Stdout, os.Stderr = arguments, realStdout, realStderr })

	os.Args = append([]string{"clankerwatch"}, args...)
	os.Stdout, os.Stderr = stdout, stderr

	code := app.Run(t.Context())

	os.Args, os.Stdout, os.Stderr = arguments, realStdout, realStderr

	require.NoError(t, stdout.Close())
	require.NoError(t, stderr.Close())

	return readFile(t, stdoutPath), readFile(t, stderrPath), code
}

// readFile returns the content of path.
func readFile(t *testing.T, path string) string {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)

	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	require.NoError(t, err)

	return string(content)
}
