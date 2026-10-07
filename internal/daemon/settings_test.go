// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// countingReload resolves settings from a file and counts its calls.
type countingReload struct {
	// resolve is the reload function under count.
	resolve Reload

	// calls is the number of calls so far.
	calls atomic.Int64
}

// commentedSettings is a settings file with comments the store must keep.
const commentedSettings = `# Clanker Watch settings.

# How often to poll while Claude Code is active.
interval: 5m

# Alert thresholds in percent.
notify: [80, 95]
`

// errReload is returned by the failing reload functions.
var errReload = errors.New("reload failed")

// TestNewSettingsStore checks the store starts with the given settings.
func TestNewSettingsStore(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, t.TempDir())
	store := newSettingsStore(cfg, nil)

	assert.Equal(t, cfg, store.Current())
	assert.Nil(t, store.reload)
}

// TestApplyNotEditable checks a store without a reload function or a
// settings file path refuses every change before touching any file.
func TestApplyNotEditable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reload Reload
		name   string
		file   bool
	}{
		{name: "no reload", reload: nil, file: true},
		{name: "no settings file", reload: func() (config.Config, error) { return config.Config{}, errReload }, file: false},
		{name: "neither", reload: nil, file: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			cfg := testConfig(t, home)

			path := filepath.Join(home, "config.yaml")
			if tt.file {
				cfg.SettingsFile = path
			}

			store := newSettingsStore(cfg, tt.reload)

			got, err := store.Apply(`{"mode":"cache-only"}`)
			require.ErrorIs(t, err, ErrNotEditable)
			assert.Equal(t, config.Config{}, got)
			assert.Equal(t, cfg, store.Current())
			assert.NoFileExists(t, path)
		})
	}
}

// TestApplyRejectsInvalidPatches checks every malformed or out-of-range
// change wraps ErrInvalidPatch and leaves the file and settings alone.
//
// The widget sends user input, so nothing that fails to decode strictly may
// reach the file.
func TestApplyRejectsInvalidPatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		patch string
	}{
		{name: "empty", patch: ""},
		{name: "not JSON", patch: "mode: cache-only"},
		{name: "unknown field", patch: `{"logLevel":"debug"}`},
		{name: "wrong type", patch: `{"intervalSeconds":"300"}`},
		{name: "fractional seconds", patch: `{"intervalSeconds":1.5}`},
		{name: "array", patch: `[]`},
		{name: "trailing object", patch: `{} {}`},
		{name: "trailing text", patch: `{"mode":"hybrid"} x`},
		{name: "over the size cap", patch: `{"mode":"` + strings.Repeat("a", maxPatchSize) + `"}`},
		{name: "negative interval", patch: `{"intervalSeconds":-1}`},
		{name: "negative idle interval", patch: `{"idleIntervalSeconds":-300}`},
		{name: "interval past a duration", patch: fmt.Sprintf(`{"intervalSeconds":%d}`, maxSeconds+1)},
		{name: "idle interval past a duration", patch: fmt.Sprintf(`{"idleIntervalSeconds":%d}`, maxSeconds+1)},
		{name: "zero threshold", patch: `{"notify":[0]}`},
		{name: "threshold over 100", patch: `{"notify":[80,150]}`},
		{name: "negative threshold", patch: `{"notify":[-5]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, reload, path := newFileStore(t, nil)
			require.NoError(t, os.WriteFile(path, []byte(commentedSettings), 0o600))

			before := store.Current()

			got, err := store.Apply(tt.patch)
			require.ErrorIs(t, err, ErrInvalidPatch)
			assert.Equal(t, config.Config{}, got)
			assert.Equal(t, before, store.Current())
			assert.Equal(t, commentedSettings, readFile(t, path))
			assert.Zero(t, reload.calls.Load(), "an invalid patch must not reload")
		})
	}
}

// TestDecodePatchRejectsTrailingClosers checks a patch followed by a stray closing
// bracket or brace is rejected like any other trailing data.
//
// [json.Decoder.More] reports false before ']' and '}', so a check that
// relies on it alone accepts these inputs although they are not JSON.
func TestDecodePatchRejectsTrailingClosers(t *testing.T) {
	t.Parallel()

	for _, patch := range []string{`{}]`, `{"mode":"cache-only"}}`, `{} ]]`} {
		t.Run(patch, func(t *testing.T) {
			t.Parallel()

			_, err := decodePatch(patch)
			require.ErrorIs(t, err, ErrInvalidPatch, "%q is not valid JSON", patch)
		})
	}
}

// TestApplyRejectsLockedKeys checks a key an environment variable or flag
// holds cannot change through the file.
//
// The file value would never take effect, so accepting it would show the
// user a setting that silently does nothing.
func TestApplyRejectsLockedKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		patch string
	}{
		{name: "mode", key: config.KeyMode, patch: `{"mode":"cache-only"}`},
		{name: "interval", key: config.KeyInterval, patch: `{"intervalSeconds":600}`},
		{name: "idle interval", key: config.KeyIdleInterval, patch: `{"idleIntervalSeconds":3600}`},
		{name: "notify", key: config.KeyNotify, patch: `{"notify":[50]}`},
		{name: "notify auth", key: config.KeyNotifyAuth, patch: `{"notifyAuth":false}`},
		{name: "one locked key among several", key: config.KeyNotify, patch: `{"mode":"cache-only","notify":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, reload, path := newFileStore(t, map[string]string{tt.key: "--" + tt.key})

			_, err := store.Apply(tt.patch)
			require.ErrorIs(t, err, ErrLocked)
			assert.Contains(t, err.Error(), tt.key)
			assert.Contains(t, err.Error(), "--"+tt.key)
			assert.NoFileExists(t, path)
			assert.Zero(t, reload.calls.Load())
		})
	}
}

// TestApplyAllowsUnlockedKeys checks a lock on one key does not block a
// change to another.
func TestApplyAllowsUnlockedKeys(t *testing.T) {
	t.Parallel()

	store, _, path := newFileStore(t, map[string]string{config.KeyInterval: config.EnvInterval})

	got, err := store.Apply(`{"mode":"cache-only"}`)
	require.NoError(t, err)
	assert.Equal(t, config.ModeCacheOnly, got.Mode)
	assert.Contains(t, readFile(t, path), "mode: cache-only")
}

// TestApplyWritesANewFile checks a change creates the settings file and
// returns the settings resolved from it.
func TestApplyWritesANewFile(t *testing.T) {
	t.Parallel()

	store, reload, path := newFileStore(t, nil)

	got, err := store.Apply(`{
		"mode": "cache-only",
		"intervalSeconds": 600,
		"idleIntervalSeconds": 5400,
		"notify": [90, 50, 90],
		"notifyAuth": false
	}`)
	require.NoError(t, err)

	assert.Equal(t, config.ModeCacheOnly, got.Mode)
	assert.Equal(t, 10*time.Minute, got.Interval)
	assert.Equal(t, 90*time.Minute, got.IdleInterval)
	assert.Equal(t, []float64{50, 90}, got.Thresholds)
	assert.False(t, got.NotifyAuth)
	assert.Equal(t, got, store.Current())
	assert.Equal(t, int64(1), reload.calls.Load())

	file, err := config.LoadFile(path)
	require.NoError(t, err)
	require.NotNil(t, file.Notify)
	assert.Equal(t, []float64{50, 90}, *file.Notify, "the file lists thresholds sorted and unique")
	require.NotNil(t, file.Interval)
	assert.Equal(t, 10*time.Minute, *file.Interval)
	require.NotNil(t, file.IdleInterval)
	assert.Equal(t, 90*time.Minute, *file.IdleInterval)
	assert.Contains(t, readFile(t, path), "idleInterval: 1h30m")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestApplyKeepsComments checks a change to an existing file keeps its
// comments and the keys the patch leaves out.
func TestApplyKeepsComments(t *testing.T) {
	t.Parallel()

	store, _, path := newFileStore(t, nil)
	require.NoError(t, os.WriteFile(path, []byte(commentedSettings), 0o600))

	got, err := store.Apply(`{"intervalSeconds":420}`)
	require.NoError(t, err)
	assert.Equal(t, 7*time.Minute, got.Interval)
	assert.Equal(t, []float64{80, 95}, got.Thresholds)

	content := readFile(t, path)
	assert.Contains(t, content, "# Clanker Watch settings.")
	assert.Contains(t, content, "# How often to poll while Claude Code is active.")
	assert.Contains(t, content, "# Alert thresholds in percent.")
	assert.Contains(t, content, "interval: 7m")
	assert.NotContains(t, content, "interval: 5m")
}

// TestApplyClearsThresholds checks an empty notify list disables alerts
// rather than counting as absent.
func TestApplyClearsThresholds(t *testing.T) {
	t.Parallel()

	store, _, path := newFileStore(t, nil)

	got, err := store.Apply(`{"notify":[]}`)
	require.NoError(t, err)
	assert.Empty(t, got.Thresholds)

	file, err := config.LoadFile(path)
	require.NoError(t, err)
	require.NotNil(t, file.Notify)
	assert.Empty(t, *file.Notify)
}

// TestApplySkipsAnUnchangedFile checks a change that leaves the file's bytes
// as they are neither writes nor reloads.
//
// The widget sends its whole page on every save, so a save without edits
// must not restart the engine with settings resolved again. The file starts
// in the layout the store writes, which is what it holds after the widget's
// first save.
func TestApplySkipsAnUnchangedFile(t *testing.T) {
	t.Parallel()

	written, err := config.UpdateFile([]byte(commentedSettings), config.File{})
	require.NoError(t, err)

	for _, patch := range []string{`{}`, `null`, `{"intervalSeconds":300}`, `{"notify":[95,80]}`} {
		t.Run(patch, func(t *testing.T) {
			t.Parallel()

			store, reload, path := newFileStore(t, nil)
			require.NoError(t, os.WriteFile(path, written, 0o600))

			old := time.Now().Add(-time.Hour).Truncate(time.Second)
			require.NoError(t, os.Chtimes(path, old, old))

			before := store.Current()

			got, err := store.Apply(patch)
			require.NoError(t, err)
			assert.Equal(t, before, got)
			assert.Equal(t, before, store.Current())
			assert.Zero(t, reload.calls.Load())
			assert.Equal(t, string(written), readFile(t, path))

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.True(t, info.ModTime().Equal(old), "the file was rewritten")
		})
	}
}

// TestApplyRollsBackAnUnusableChange checks a change the reload rejects
// restores the previous file byte for byte.
//
// Only the full resolution knows that an interval is below the minimum or
// shorter than the idle interval, so the file is written first and must be
// put back.
func TestApplyRollsBackAnUnusableChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want  error
		name  string
		patch string
	}{
		{name: "interval below the minimum", patch: `{"intervalSeconds":60}`, want: config.ErrIntervalTooShort},
		{name: "zero interval", patch: `{"intervalSeconds":0}`, want: config.ErrIntervalTooShort},
		{name: "idle shorter than active", patch: `{"idleIntervalSeconds":180}`, want: config.ErrIdleTooShort},
		{name: "unknown mode", patch: `{"mode":"bogus"}`, want: config.ErrInvalidMode},
	}

	for _, tt := range tests {
		t.Run(tt.name+" restores the file", func(t *testing.T) {
			t.Parallel()

			store, reload, path := newFileStore(t, nil)
			require.NoError(t, os.WriteFile(path, []byte(commentedSettings), 0o600))

			before := store.Current()

			got, err := store.Apply(tt.patch)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, config.Config{}, got)
			assert.Equal(t, before, store.Current())
			assert.Equal(t, commentedSettings, readFile(t, path))
			assert.Equal(t, int64(1), reload.calls.Load())
		})

		t.Run(tt.name+" removes a new file", func(t *testing.T) {
			t.Parallel()

			store, _, path := newFileStore(t, nil)

			_, err := store.Apply(tt.patch)
			require.ErrorIs(t, err, tt.want)
			assert.NoFileExists(t, path)

			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			assert.Empty(t, entries, "no temporary file is left behind")
		})
	}
}

// TestApplyReportsAFailedRestore checks a rollback that cannot write is
// reported together with the reload error.
func TestApplyReportsAFailedRestore(t *testing.T) {
	t.Parallel()

	for _, existed := range []bool{true, false} {
		t.Run(fmt.Sprintf("existed=%v", existed), func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "settings")
			require.NoError(t, os.Mkdir(dir, 0o700))

			path := filepath.Join(dir, "config.yaml")
			if existed {
				require.NoError(t, os.WriteFile(path, []byte(commentedSettings), 0o600))
			}

			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

			cfg := testConfig(t, t.TempDir())
			cfg.SettingsFile = path

			store := newSettingsStore(cfg, func() (config.Config, error) {
				// A read-only directory makes both the restore and the
				// removal of a new file fail.
				require.NoError(t, os.Chmod(dir, 0o500))

				return config.Config{}, errReload
			})

			_, err := store.Apply(`{"mode":"cache-only"}`)
			require.ErrorIs(t, err, errReload)

			if existed {
				assert.Contains(t, err.Error(), "settings file")
			} else {
				assert.Contains(t, err.Error(), "remove the settings file")
			}

			assert.Equal(t, cfg, store.Current())
		})
	}
}

// TestApplyReportsFileErrors checks failures to read, update, or save the
// file are wrapped and leave the settings alone.
func TestApplyReportsFileErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(t *testing.T, dir string) string
		want  error
		name  string
		text  string
	}{
		{
			name: "unreadable",
			text: "read the settings file",
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				path := filepath.Join(dir, "config.yaml")
				require.NoError(t, os.Mkdir(path, 0o700))

				return path
			},
		},
		{
			name: "invalid content",
			text: "update the settings file",
			want: config.ErrInvalidFile,
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				path := filepath.Join(dir, "config.yaml")
				require.NoError(t, os.WriteFile(path, []byte("bogus: 1\n"), 0o600))

				return path
			},
		},
		{
			name: "read-only directory",
			text: "save the settings file",
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				locked := filepath.Join(dir, "locked")
				require.NoError(t, os.Mkdir(locked, 0o500))
				t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

				return filepath.Join(locked, "config.yaml")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reload := &countingReload{resolve: func() (config.Config, error) { return config.Config{}, errReload }}
			cfg := testConfig(t, t.TempDir())
			cfg.SettingsFile = tt.setup(t, t.TempDir())

			store := newSettingsStore(cfg, reload.call)

			got, err := store.Apply(`{"mode":"cache-only"}`)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.text)

			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}

			assert.Equal(t, config.Config{}, got)
			assert.Equal(t, cfg, store.Current())
			assert.Zero(t, reload.calls.Load())
		})
	}
}

// TestApplyConcurrently checks parallel changes serialize on the store.
//
// D-Bus delivers each call on its own goroutine, so two widgets saving at
// once must each see a consistent file and the store must end on one of
// their results. Run with -race.
func TestApplyConcurrently(t *testing.T) {
	t.Parallel()

	store, reload, path := newFileStore(t, nil)

	const writers = 8

	var wg sync.WaitGroup

	results := make([]config.Config, writers)
	errs := make([]error, writers)

	for i := range writers {
		wg.Go(func() {
			results[i], errs[i] = store.Apply(fmt.Sprintf(`{"intervalSeconds":%d}`, 180+60*i))
			_ = store.View()
			_ = store.Current()
		})
	}

	wg.Wait()

	for i := range writers {
		require.NoError(t, errs[i])
		assert.Equal(t, time.Duration(180+60*i)*time.Second, results[i].Interval)
	}

	assert.Equal(t, int64(writers), reload.calls.Load())

	file, err := config.LoadFile(path)
	require.NoError(t, err)
	require.NotNil(t, file.Interval)
	assert.Equal(t, *file.Interval, store.Current().Interval, "the store holds the last write")
}

// TestReload checks a reload replaces the settings on success and keeps them
// on failure.
func TestReload(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		store, reload, path := newFileStore(t, nil)
		require.NoError(t, os.WriteFile(path, []byte("mode: cache-only\n"), 0o600))

		got, err := store.Reload()
		require.NoError(t, err)
		assert.Equal(t, config.ModeCacheOnly, got.Mode)
		assert.Equal(t, got, store.Current())
		assert.Equal(t, int64(1), reload.calls.Load())
	})

	t.Run("invalid file", func(t *testing.T) {
		t.Parallel()

		store, _, path := newFileStore(t, nil)
		require.NoError(t, os.WriteFile(path, []byte("interval: 1s\n"), 0o600))

		before := store.Current()

		got, err := store.Reload()
		require.ErrorIs(t, err, config.ErrIntervalTooShort)
		assert.Equal(t, config.Config{}, got)
		assert.Equal(t, before, store.Current())
	})

	t.Run("not editable", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig(t, t.TempDir())
		store := newSettingsStore(cfg, nil)

		got, err := store.Reload()
		require.ErrorIs(t, err, ErrNotEditable)
		assert.Equal(t, config.Config{}, got)
		assert.Equal(t, cfg, store.Current())
	})
}

// TestView checks the Settings property's shape for the cases the widget
// distinguishes.
//
// The plasmoid indexes notify and locked directly, so both must be JSON
// arrays and objects even when empty, never null.
func TestView(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	reload := func() (config.Config, error) { return config.Config{}, errReload }

	defaults := testConfig(t, home)
	defaults.SettingsFile = filepath.Join(home, "config.yaml")

	empty := defaults
	empty.Thresholds = nil
	empty.Overrides = nil
	empty.Mode = config.ModeCacheOnly
	empty.NotifyAuth = false
	empty.Interval = 3*time.Minute + 30*time.Second
	empty.IdleInterval = time.Hour

	locked := defaults
	locked.Overrides = map[string]string{config.KeyInterval: config.EnvInterval, config.KeyMode: "--mode"}

	noFile := defaults
	noFile.SettingsFile = ""

	tests := []struct {
		reload Reload
		name   string
		want   string
		cfg    config.Config
	}{
		{
			name: "defaults", cfg: defaults, reload: reload,
			want: `{"v":1,"file":"` + defaults.SettingsFile + `","mode":"hybrid","intervalSeconds":300,
				"idleIntervalSeconds":1200,"minIntervalSeconds":120,"notify":[80,95],"notifyAuth":true,
				"locked":{},"editable":true}`,
		},
		{
			name: "nil lists", cfg: empty, reload: reload,
			want: `{"v":1,"file":"` + defaults.SettingsFile + `","mode":"cache-only","intervalSeconds":210,
				"idleIntervalSeconds":3600,"minIntervalSeconds":120,"notify":[],"notifyAuth":false,
				"locked":{},"editable":true}`,
		},
		{
			name: "locked keys", cfg: locked, reload: reload,
			want: `{"v":1,"file":"` + defaults.SettingsFile + `","mode":"hybrid","intervalSeconds":300,
				"idleIntervalSeconds":1200,"minIntervalSeconds":120,"notify":[80,95],"notifyAuth":true,
				"locked":{"interval":"CLANKERWATCH_INTERVAL","mode":"--mode"},"editable":true}`,
		},
		{
			name: "no reload", cfg: defaults, reload: nil,
			want: `{"v":1,"file":"` + defaults.SettingsFile + `","mode":"hybrid","intervalSeconds":300,
				"idleIntervalSeconds":1200,"minIntervalSeconds":120,"notify":[80,95],"notifyAuth":true,
				"locked":{},"editable":false}`,
		},
		{
			name: "no settings file", cfg: noFile, reload: reload,
			want: `{"v":1,"file":"","mode":"hybrid","intervalSeconds":300,
				"idleIntervalSeconds":1200,"minIntervalSeconds":120,"notify":[80,95],"notifyAuth":true,
				"locked":{},"editable":false}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			view := newSettingsStore(tt.cfg, tt.reload).View()

			assert.JSONEq(t, tt.want, view)

			var fields map[string]json.RawMessage

			require.NoError(t, json.Unmarshal([]byte(view), &fields))
			assert.Len(t, fields, 10, "the view has exactly the documented fields")
		})
	}
}

// TestViewFollowsApply checks the view reflects a successful change.
func TestViewFollowsApply(t *testing.T) {
	t.Parallel()

	store, _, _ := newFileStore(t, nil)

	_, err := store.Apply(`{"intervalSeconds":240,"notify":[70]}`)
	require.NoError(t, err)

	var view settingsView

	require.NoError(t, json.Unmarshal([]byte(store.View()), &view))
	assert.Equal(t, int64(240), view.IntervalSeconds)
	assert.Equal(t, []float64{70}, view.Notify)
	assert.True(t, view.Editable)
}

// call runs the reload function and counts the call.
func (r *countingReload) call() (config.Config, error) {
	r.calls.Add(1)

	return r.resolve()
}

// newFileStore returns a store over a settings file in a temporary
// directory, the counting reload behind it, and the file path.
//
// The reload resolves the defaults, the file, and the locked keys, then
// validates, the way the serve command does without flags or environment.
func newFileStore(t *testing.T, locked map[string]string) (*settingsStore, *countingReload, string) {
	t.Helper()

	home := t.TempDir()
	dir := filepath.Join(home, "config")
	require.NoError(t, os.Mkdir(dir, 0o700))

	path := filepath.Join(dir, "config.yaml")
	reload := &countingReload{resolve: fileReload(t, home, path, locked)}

	cfg, err := fileReload(t, home, path, locked)()
	require.NoError(t, err)

	return newSettingsStore(cfg, reload.call), reload, path
}

// fileReload returns a reload function that resolves the settings file at
// path over the defaults for home.
func fileReload(t *testing.T, home, path string, locked map[string]string) Reload {
	t.Helper()

	return func() (config.Config, error) {
		cfg := testConfig(t, home)
		cfg.SettingsFile = path

		if locked != nil {
			cfg.Overrides = locked
		}

		file, err := config.LoadOptionalFile(path)
		if err != nil {
			return config.Config{}, err
		}

		err = file.Apply(&cfg)
		if err != nil {
			return config.Config{}, err
		}

		err = cfg.Validate()
		if err != nil {
			return config.Config{}, err
		}

		return cfg, nil
	}
}

// readFile returns the content of path.
func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(data)
}
