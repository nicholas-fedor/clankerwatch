// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commentedKey matches a commented-out key line in the example, such as
// "#interval: 5m", and captures the key.
var commentedKey = regexp.MustCompile(`^#([a-zA-Z]+):`)

// fullPatch returns a patch that sets every key to a value other than its
// default.
//
// Returns:
//   - File: a patch with every field present.
func fullPatch() File {
	return File{
		Mode:            new("cache-only"),
		Interval:        new(3 * time.Minute),
		IdleInterval:    new(90 * time.Minute),
		Notify:          new([]float64{80, 95}),
		NotifyAuth:      new(false),
		LogLevel:        new("debug"),
		ClaudeConfigDir: new(""),
		StateDir:        new(""),
	}
}

// fullPatchLines maps each key to the line fullPatch renders for it.
//
// Returns:
//   - map[string]string: lines keyed by settings file key.
func fullPatchLines() map[string]string {
	return map[string]string{
		KeyMode:            "mode: cache-only",
		KeyInterval:        "interval: 3m",
		KeyIdleInterval:    "idleInterval: 1h30m",
		KeyNotify:          "notify: [80, 95]",
		KeyNotifyAuth:      "notifyAuth: false",
		KeyLogLevel:        "logLevel: debug",
		KeyClaudeConfigDir: `claudeConfigDir: ""`,
		KeyStateDir:        `stateDir: ""`,
	}
}

// readExample reads the shipped example settings file.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - []byte: the example content.
func readExample(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile(exampleFile)
	require.NoError(t, err)

	return data
}

// update applies a patch and requires the result to decode.
//
// Parameters:
//   - t: test handle.
//   - data: the current content.
//   - patch: the settings to set.
//
// Returns:
//   - string: the updated content.
//   - File: the updated content decoded.
func update(t *testing.T, data string, patch File) (string, File) {
	t.Helper()

	out, err := UpdateFile([]byte(data), patch)
	require.NoError(t, err, "update %q", data)

	file, err := DecodeFile(out)
	require.NoError(t, err, "decode %q", out)

	return string(out), file
}

// TestUpdateFileExampleEveryKey sets every key in the shipped example.
//
// Each key must land on its documented commented-out line, so its comment
// stays above it, and every other line must survive unchanged.
func TestUpdateFileExampleEveryKey(t *testing.T) {
	t.Parallel()

	example := readExample(t)
	got, file := update(t, string(example), fullPatch())

	assert.Equal(t, fullPatch(), file)

	before := strings.Split(string(example), "\n")
	after := strings.Split(got, "\n")
	require.Len(t, after, len(before), "the update adds and removes no lines")

	lines := fullPatchLines()
	seen := map[string]bool{}

	for index, line := range before {
		match := commentedKey.FindStringSubmatch(line)
		if match == nil {
			assert.Equal(t, line, after[index], "line %d is kept", index+1)

			continue
		}

		key := match[1]
		seen[key] = true

		assert.Equal(t, lines[key], after[index], "%s replaces its documented line %d", key, index+1)
	}

	assert.Len(t, seen, len(lines), "the example documents every key")
}

// TestUpdateFileExampleOneKey sets a single key in the shipped example and
// leaves the other documented keys commented out.
func TestUpdateFileExampleOneKey(t *testing.T) {
	t.Parallel()

	example := string(readExample(t))
	got, file := update(t, example, File{Interval: new(10 * time.Minute)})

	assert.Equal(t, File{Interval: new(10 * time.Minute)}, file)
	assert.Equal(t, strings.Replace(example, "\n#interval: 5m\n", "\ninterval: 10m\n", 1), got)
}

// TestUpdateFileEmpty writes every key in file order into empty content.
func TestUpdateFileEmpty(t *testing.T) {
	t.Parallel()

	got, file := update(t, "", fullPatch())

	assert.Equal(t, fullPatch(), file)
	assert.Equal(t, `mode: cache-only
interval: 3m
idleInterval: 1h30m
notify: [80, 95]
notifyAuth: false
logLevel: debug
claudeConfigDir: ""
stateDir: ""
`, got)
}

// TestUpdateFileNoDocument sets keys in content the decoder treats as holding
// no settings.
//
// A null document such as "~" or "--- ~" and a lone byte order mark decode as
// an empty file, so they must accept settings like an empty file does.
func TestUpdateFileNoDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "blank line", content: "\n"},
		{name: "comment without newline", content: "# only a comment"},
		{name: "document start", content: "---\n"},
		{name: "document end", content: "...\n"},
		{name: "utf-8 byte order mark", content: "\xef\xbb\xbf"},
		{name: "tilde", content: "~\n"},
		{name: "null after document start", content: "--- ~\n"},
		{name: "null word", content: "null\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			before, err := DecodeFile([]byte(test.content))
			require.NoError(t, err)
			require.Equal(t, File{}, before, "the content holds no settings")

			out, err := UpdateFile([]byte(test.content), File{Interval: new(3 * time.Minute)})
			require.NoError(t, err, "%q", test.content)

			file, err := DecodeFile(out)
			require.NoError(t, err, "%q gave %q", test.content, out)
			assert.Equal(t, File{Interval: new(3 * time.Minute)}, file, "%q gave %q", test.content, out)
		})
	}
}

// TestUpdateFileCommentOnlyAppends appends a key that has no commented-out
// line, keeping the comment and adding the missing final newline.
func TestUpdateFileCommentOnlyAppends(t *testing.T) {
	t.Parallel()

	got, _ := update(t, "# my settings", File{LogLevel: new("warn")})
	assert.Equal(t, "# my settings\nlogLevel: warn\n", got)
}

// TestUpdateFileKeepsComments edits a mapping without losing its comments.
//
// Head, line, foot, and trailing comments must all survive, a replaced value
// keeps the comment on its line, and the blank lines that separate sections
// stay.
func TestUpdateFileKeepsComments(t *testing.T) {
	t.Parallel()

	content := `# Settings for this machine.

# Data source.
mode: hybrid # chosen for the laptop
interval: 5m # matches the default

# Thresholds.
notify: [50]

# Trailing note.
`

	got, file := update(t, content, File{Mode: new("cache-only"), LogLevel: new("debug")})

	for _, want := range []string{
		"# Settings for this machine.\n",
		"# Data source.\nmode: cache-only # chosen for the laptop\n",
		"interval: 5m # matches the default\n\n# Thresholds.\nnotify: [50]\n",
		"# Trailing note.\n",
		"logLevel: debug\n",
	} {
		assert.Contains(t, got, want)
	}

	assert.Equal(t, File{
		Mode:            new("cache-only"),
		Interval:        new(5 * time.Minute),
		IdleInterval:    nil,
		Notify:          new([]float64{50}),
		NotifyAuth:      nil,
		LogLevel:        new("debug"),
		ClaudeConfigDir: nil,
		StateDir:        nil,
	}, file)
}

// TestUpdateFileAppendsKey adds a key the mapping lacks after the existing
// ones.
func TestUpdateFileAppendsKey(t *testing.T) {
	t.Parallel()

	got, _ := update(t, "mode: hybrid\n", File{Interval: new(3 * time.Minute)})
	assert.Equal(t, "mode: hybrid\ninterval: 3m\n", got)
}

// TestUpdateFileValues renders each kind of value on one line in both
// content without a document and a mapping.
//
// Lists use flow style so they fit the example's one-line notify. Strings
// that YAML would read as another type, such as "" or "null", are quoted.
func TestUpdateFileValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		patch File
		name  string
		line  string
	}{
		{name: "empty notify", patch: File{Notify: new([]float64{})}, line: "notify: []"},
		{name: "notify list", patch: File{Notify: new([]float64{80, 95})}, line: "notify: [80, 95]"},
		{name: "fractional notify", patch: File{Notify: new([]float64{12.5})}, line: "notify: [12.5]"},
		{name: "empty string", patch: File{ClaudeConfigDir: new("")}, line: `claudeConfigDir: ""`},
		{name: "null string", patch: File{StateDir: new("null")}, line: `stateDir: "null"`},
		{name: "boolean string", patch: File{Mode: new("true")}, line: `mode: "true"`},
		{name: "true", patch: File{NotifyAuth: new(true)}, line: "notifyAuth: true"},
		{name: "duration", patch: File{IdleInterval: new(time.Hour)}, line: "idleInterval: 1h"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, content := range []string{"", "# comment\n", "logLevel: info\n"} {
				got, file := update(t, content, test.patch)
				assert.Contains(t, strings.Split(got, "\n"), test.line, "%q", content)

				// No patch sets logLevel, so drop the one the mapping holds.
				file.LogLevel = nil
				assert.Equal(t, test.patch, file, "%q", content)
			}
		})
	}
}

// TestUpdateFileReplacesBlockSequence replaces a notify list written in block
// style with a flow list.
func TestUpdateFileReplacesBlockSequence(t *testing.T) {
	t.Parallel()

	got, file := update(t, "notify:\n  - 50\n  - 60\nmode: hybrid\n", File{Notify: new([]float64{70, 90})})

	assert.Equal(t, "notify: [70, 90]\nmode: hybrid\n", got)
	assert.Equal(t, new([]float64{70, 90}), file.Notify)
	assert.Equal(t, new("hybrid"), file.Mode)
}

// TestUpdateFileRejectsAnchors refuses a file that uses anchors and aliases.
//
// Replacing an anchored value would either break the alias or silently change
// the key that refers to it, so the file must be edited by hand.
func TestUpdateFileRejectsAnchors(t *testing.T) {
	t.Parallel()

	_, err := UpdateFile([]byte("interval: &poll 5m\nidleInterval: *poll\n"), File{Interval: new(3 * time.Minute)})
	require.ErrorIs(t, err, ErrInvalidFile)
	require.ErrorIs(t, err, ErrUnsupportedYAML)
}

// TestUpdateFileRejectsUTF16 refuses content that is not UTF-8, such as a
// UTF-16 byte order mark, rather than appending UTF-8 text to it.
func TestUpdateFileRejectsUTF16(t *testing.T) {
	t.Parallel()

	_, err := UpdateFile([]byte("\xfe\xff"), File{Interval: new(3 * time.Minute)})
	require.ErrorIs(t, err, ErrInvalidFile)
	require.ErrorIs(t, err, ErrNotUTF8)
}

// TestUpdateFileRejectsInvalid refuses content the strict decoder refuses,
// so an update never hides a broken file behind a rewrite.
//
// A null key and a merge key document are not settings, so they must be
// refused rather than failing on the rewrite or dropping the patch.
func TestUpdateFileRejectsInvalid(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"bogus: 1\n",
		"notify: [80\n",
		"mode: a\nmode: b\n",
		"[1, 2]\n",
		"interval: 5m\n---\ninterval: 9m\n",
		"interval: soon\n",
		"~: 1\n",
		"? \n",
		"<<\n",
	} {
		got, err := UpdateFile([]byte(content), fullPatch())
		require.ErrorIs(t, err, ErrInvalidFile, "%q", content)
		assert.Nil(t, got, "%q", content)
	}
}

// TestUpdateFileIdempotent applies the same patch twice.
//
// The settings dialog saves whole forms, so saving unchanged values must not
// churn the file.
func TestUpdateFileIdempotent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		patch   File
		name    string
		content string
	}{
		{name: "example every key", content: string(readExample(t)), patch: fullPatch()},
		{name: "example one key", content: string(readExample(t)), patch: File{Notify: new([]float64{})}},
		{name: "empty", content: "", patch: fullPatch()},
		{name: "mapping", content: "# head\nmode: hybrid # line\n\n# tail\n", patch: fullPatch()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			once, _ := update(t, test.content, test.patch)
			twice, _ := update(t, once, test.patch)

			assert.Equal(t, once, twice)
		})
	}
}

// TestUpdateFileEmptyPatch leaves the decoded settings unchanged.
func TestUpdateFileEmptyPatch(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"",
		string(readExample(t)),
		"mode: cache-only # line\ninterval: 3m\nnotify:\n  - 50\nstateDir: \"\"\n",
	} {
		want, err := DecodeFile([]byte(content))
		require.NoError(t, err)

		_, got := update(t, content, File{})
		assert.Equal(t, want, got, "%q", content)
	}
}

// TestSaveFileCreatesDirectory writes a new file with private permissions in
// a directory it creates.
func TestSaveFileCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "config", "clankerwatch")
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, SaveFile(path, []byte("interval: 3m\n")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "interval: 3m\n", string(got))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	info, err = os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	assertOnlyFile(t, dir, "config.yaml")
}

// TestSaveFileReplacesAtomically swaps in a new file rather than rewriting the
// old one.
//
// A reader that opened the old file keeps its content, which shows the new
// content never passes through a half-written file at the same path. The
// replacement also tightens a looser mode to 0600.
func TestSaveFileReplacesAtomically(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("interval: 5m\n"), 0o644))

	reader, err := os.Open(path)
	require.NoError(t, err)

	t.Cleanup(func() { _ = reader.Close() })

	before, err := reader.Stat()
	require.NoError(t, err)

	require.NoError(t, SaveFile(path, []byte("interval: 3m\n")))

	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.False(t, os.SameFile(before, after), "the path names a new file")
	assert.Equal(t, os.FileMode(0o600), after.Mode().Perm())

	old := make([]byte, 64)
	count, err := reader.Read(old)
	require.NoError(t, err)
	assert.Equal(t, "interval: 5m\n", string(old[:count]))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "interval: 3m\n", string(got))

	assertOnlyFile(t, dir, "config.yaml")
}

// TestSaveFileDirectoryError reports a directory that cannot be created
// because a regular file sits in its path.
func TestSaveFileDirectoryError(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	err := SaveFile(filepath.Join(blocker, "sub", "config.yaml"), []byte("interval: 3m\n"))
	require.ErrorIs(t, err, syscall.ENOTDIR)
	assert.Contains(t, err.Error(), "create the settings directory")
}

// TestSaveFileUnwritableDirectory reports a directory where no temporary file
// can be created.
func TestSaveFileUnwritableDirectory(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root writes to read-only directories")
	}

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := SaveFile(filepath.Join(dir, "config.yaml"), []byte("interval: 3m\n"))
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Contains(t, err.Error(), "create a temporary settings file")
}

// TestSaveFileRenameError reports a path that cannot be replaced and removes
// the temporary file.
func TestSaveFileRenameError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.Mkdir(path, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "keep"), nil, 0o600))

	err := SaveFile(path, []byte("interval: 3m\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace the settings file")

	assertOnlyFile(t, dir, "config.yaml")
}

// TestFormatDuration writes durations without zero units and in a form
// time.ParseDuration reads back.
func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want     string
		duration time.Duration
	}{
		{duration: 5 * time.Minute, want: "5m"},
		{duration: 20 * time.Minute, want: "20m"},
		{duration: 90 * time.Minute, want: "1h30m"},
		{duration: time.Hour, want: "1h"},
		{duration: 90 * time.Second, want: "1m30s"},
		{duration: 30 * time.Second, want: "30s"},
		{duration: 2*time.Hour + 30*time.Second, want: "2h0m30s"},
		{duration: 1500 * time.Millisecond, want: "1.5s"},
		{duration: 0, want: "0s"},
	}

	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()

			got := FormatDuration(test.duration)
			assert.Equal(t, test.want, got)

			parsed, err := time.ParseDuration(got)
			require.NoError(t, err)
			assert.Equal(t, test.duration, parsed)
		})
	}
}

// TestEntriesOrder lists present keys in the order the example documents
// them, with durations already formatted.
func TestEntriesOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []entry{
		{key: KeyMode, value: "cache-only"},
		{key: KeyInterval, value: "3m"},
		{key: KeyIdleInterval, value: "1h30m"},
		{key: KeyNotify, value: []float64{80, 95}},
		{key: KeyNotifyAuth, value: false},
		{key: KeyLogLevel, value: "debug"},
		{key: KeyClaudeConfigDir, value: ""},
		{key: KeyStateDir, value: ""},
	}, fullPatch().entries())

	assert.Equal(t, []entry{{key: KeyNotifyAuth, value: true}}, File{NotifyAuth: new(true)}.entries())
	assert.Empty(t, File{}.entries())
}

// assertOnlyFile checks a directory holds exactly one entry, so no temporary
// file is left behind.
//
// Parameters:
//   - t: test handle.
//   - dir: the directory.
//   - name: the only expected entry.
func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, item := range entries {
		names = append(names, item.Name())
	}

	assert.Equal(t, []string{name}, names)
}
