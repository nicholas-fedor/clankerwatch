// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// settingsView is the Settings property: the settings the widget can change,
// their effective values, and which of them an environment variable or flag
// holds.
type settingsView struct {
	// Locked maps each key the settings file cannot change to its source.
	Locked map[string]string `json:"locked"`

	// File is the settings file path.
	File string `json:"file"`

	// Mode is the data source.
	Mode config.Mode `json:"mode"`

	// Notify lists the alert thresholds in percent. Empty means no alerts.
	Notify []float64 `json:"notify"`

	// V is the schema version.
	V int `json:"v"`

	// IntervalSeconds is the active polling interval.
	IntervalSeconds int64 `json:"intervalSeconds"`

	// IdleIntervalSeconds is the idle polling interval.
	IdleIntervalSeconds int64 `json:"idleIntervalSeconds"`

	// MinIntervalSeconds is the shortest allowed polling interval.
	MinIntervalSeconds int64 `json:"minIntervalSeconds"`

	// NotifyAuth enables the login alert.
	NotifyAuth bool `json:"notifyAuth"`

	// Editable is false when the daemon cannot change settings, such as in
	// demo mode or without a settings file path.
	Editable bool `json:"editable"`
}

// settingsPatch is the SetSettings argument. Absent fields stay unchanged.
type settingsPatch struct {
	// Mode is the data source.
	Mode *string `json:"mode"`

	// IntervalSeconds is the active polling interval.
	IntervalSeconds *int64 `json:"intervalSeconds"`

	// IdleIntervalSeconds is the idle polling interval.
	IdleIntervalSeconds *int64 `json:"idleIntervalSeconds"`

	// Notify lists the alert thresholds in percent.
	Notify *[]float64 `json:"notify"`

	// NotifyAuth enables the login alert.
	NotifyAuth *bool `json:"notifyAuth"`
}

// settingsStore changes the settings file on behalf of the widget.
//
// It writes the patch, resolves the settings again the way the daemon
// started, and restores the previous file when the result is not usable.
type settingsStore struct {
	reload Reload
	cfg    config.Config
	mu     sync.Mutex
}

const (
	// settingsVersion is the Settings property's schema version.
	settingsVersion = 1

	// maxPatchSize bounds the SetSettings argument.
	maxPatchSize = 4 << 10

	// maxSeconds is the longest interval a duration can hold, in seconds.
	maxSeconds = int64(math.MaxInt64 / time.Second)
)

var (
	// ErrNotEditable indicates that the daemon cannot change settings now.
	ErrNotEditable = errors.New("settings cannot be changed here")

	// ErrLocked indicates a patch for a key an environment variable or flag holds.
	ErrLocked = errors.New("setting is held by the environment or a flag")

	// ErrInvalidPatch indicates a SetSettings argument that does not decode.
	ErrInvalidPatch = errors.New("invalid settings change")
)

// newSettingsStore returns a store for the running settings.
//
// Parameters:
//   - cfg: the settings the daemon runs with.
//   - reload: resolves the settings again, or nil when they cannot change.
//
// Returns:
//   - *settingsStore: the store.
func newSettingsStore(cfg config.Config, reload Reload) *settingsStore {
	return &settingsStore{reload: reload, cfg: cfg, mu: sync.Mutex{}}
}

// Apply writes a settings change and returns the settings that result.
//
// Parameters:
//   - patchJSON: the change as JSON.
//
// Returns:
//   - config.Config: the resolved settings after the change.
//   - error: ErrNotEditable, ErrInvalidPatch, ErrLocked, a file error, or the
//     validation error that made the change roll back.
func (s *settingsStore) Apply(patchJSON string) (config.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.editable() {
		return config.Config{}, ErrNotEditable
	}

	patch, err := decodePatch(patchJSON)
	if err != nil {
		return config.Config{}, err
	}

	file, err := patch.file(s.cfg.Overrides)
	if err != nil {
		return config.Config{}, err
	}

	cfg, err := s.write(file)
	if err != nil {
		return config.Config{}, err
	}

	s.cfg = cfg

	return cfg, nil
}

// Current returns the settings the store holds.
//
// Returns:
//   - config.Config: the settings.
func (s *settingsStore) Current() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cfg
}

// Reload resolves the settings again after the file changed by hand.
//
// Returns:
//   - config.Config: the resolved settings.
//   - error: ErrNotEditable, or the resolution error. The running settings
//     stay in place on error.
func (s *settingsStore) Reload() (config.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.reload == nil {
		return config.Config{}, ErrNotEditable
	}

	cfg, err := s.reload()
	if err != nil {
		return config.Config{}, err
	}

	s.cfg = cfg

	return cfg, nil
}

// View returns the Settings property JSON for the current settings.
//
// Returns:
//   - string: the JSON document.
func (s *settingsStore) View() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	notify := s.cfg.Thresholds
	if notify == nil {
		notify = []float64{}
	}

	locked := s.cfg.Overrides
	if locked == nil {
		locked = map[string]string{}
	}

	data, err := json.Marshal(settingsView{
		Locked:              locked,
		File:                s.cfg.SettingsFile,
		Mode:                s.cfg.Mode,
		Notify:              notify,
		V:                   settingsVersion,
		IntervalSeconds:     int64(s.cfg.Interval / time.Second),
		IdleIntervalSeconds: int64(s.cfg.IdleInterval / time.Second),
		MinIntervalSeconds:  int64(config.MinInterval / time.Second),
		NotifyAuth:          s.cfg.NotifyAuth,
		Editable:            s.editable(),
	})
	if err != nil {
		// The view holds only strings, numbers, and booleans, which always
		// encode. NaN thresholds never pass validation.
		return "{}"
	}

	return string(data)
}

// editable reports whether the store can change settings.
//
// Returns:
//   - bool: true with a reload function and a settings file path.
func (s *settingsStore) editable() bool {
	return s.reload != nil && s.cfg.SettingsFile != ""
}

// write updates the settings file and resolves the result, restoring the
// previous file when the result is not usable.
//
// Parameters:
//   - file: the keys to set.
//
// Returns:
//   - config.Config: the resolved settings.
//   - error: a file error or the resolution error.
func (s *settingsStore) write(file config.File) (config.Config, error) {
	path := s.cfg.SettingsFile

	previous, err := os.ReadFile(path)
	existed := err == nil

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return config.Config{}, fmt.Errorf("read the settings file: %w", err)
	}

	updated, err := config.UpdateFile(previous, file)
	if err != nil {
		return config.Config{}, fmt.Errorf("update the settings file: %w", err)
	}

	if existed && bytes.Equal(previous, updated) {
		return s.cfg, nil
	}

	err = config.SaveFile(path, updated)
	if err != nil {
		return config.Config{}, fmt.Errorf("save the settings file: %w", err)
	}

	cfg, err := s.reload()
	if err == nil {
		return cfg, nil
	}

	if existed {
		return config.Config{}, errors.Join(err, restore(path, previous))
	}

	return config.Config{}, errors.Join(err, removeNew(path))
}

// decodePatch strictly decodes a SetSettings argument.
//
// Parameters:
//   - patchJSON: the change as JSON.
//
// Returns:
//   - settingsPatch: the change.
//   - error: an error wrapping ErrInvalidPatch.
func decodePatch(patchJSON string) (settingsPatch, error) {
	var patch settingsPatch

	if len(patchJSON) > maxPatchSize {
		return patch, fmt.Errorf("%w: over %d bytes", ErrInvalidPatch, maxPatchSize)
	}

	// Decoder.More misses trailing closers such as "{}]", so the whole
	// argument must be one valid JSON value first.
	if !json.Valid([]byte(patchJSON)) {
		return patch, fmt.Errorf("%w: not a single JSON value", ErrInvalidPatch)
	}

	decoder := json.NewDecoder(bytes.NewReader([]byte(patchJSON)))
	decoder.DisallowUnknownFields()

	err := decoder.Decode(&patch)
	if err != nil {
		return settingsPatch{}, fmt.Errorf("%w: %w", ErrInvalidPatch, err)
	}

	if decoder.More() {
		return settingsPatch{}, fmt.Errorf("%w: trailing data", ErrInvalidPatch)
	}

	return patch, nil
}

// file converts the patch to settings file keys.
//
// Parameters:
//   - locked: the keys an environment variable or flag holds.
//
// Returns:
//   - config.File: the keys to set.
//   - error: ErrLocked for a key in locked, or ErrInvalidPatch for an
//     interval or threshold out of range.
func (p settingsPatch) file(locked map[string]string) (config.File, error) {
	interval, err := seconds(p.IntervalSeconds)
	if err != nil {
		return config.File{}, err
	}

	idle, err := seconds(p.IdleIntervalSeconds)
	if err != nil {
		return config.File{}, err
	}

	notify, err := thresholds(p.Notify)
	if err != nil {
		return config.File{}, err
	}

	file := config.File{
		Mode:            p.Mode,
		Interval:        interval,
		IdleInterval:    idle,
		Notify:          notify,
		NotifyAuth:      p.NotifyAuth,
		LogLevel:        nil,
		ClaudeConfigDir: nil,
		StateDir:        nil,
	}

	present := map[string]bool{
		config.KeyMode:         p.Mode != nil,
		config.KeyInterval:     p.IntervalSeconds != nil,
		config.KeyIdleInterval: p.IdleIntervalSeconds != nil,
		config.KeyNotify:       p.Notify != nil,
		config.KeyNotifyAuth:   p.NotifyAuth != nil,
	}

	keys := make([]string, 0, len(present))
	for key := range present {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for _, key := range keys {
		if source, held := locked[key]; held && present[key] {
			return config.File{}, fmt.Errorf("%w: %s is set by %s", ErrLocked, key, source)
		}
	}

	return file, nil
}

// seconds converts optional seconds to an optional duration.
//
// Parameters:
//   - value: seconds, or nil.
//
// Returns:
//   - *[time.Duration]: the duration, or nil.
//   - error: ErrInvalidPatch for a negative value or one too large for a
//     duration, which would otherwise wrap around.
func seconds(value *int64) (*time.Duration, error) {
	if value == nil {
		return nil, nil //nolint:nilnil // An absent value is not an error.
	}

	if *value < 0 || *value > maxSeconds {
		return nil, fmt.Errorf("%w: %d seconds is out of range", ErrInvalidPatch, *value)
	}

	return new(time.Duration(*value) * time.Second), nil
}

// thresholds sorts optional alert thresholds, so the file lists them in order.
//
// Parameters:
//   - value: thresholds in any order, or nil.
//
// Returns:
//   - *[]float64: ascending, unique thresholds, or nil.
//   - error: ErrInvalidPatch for a threshold outside (0, 100].
func thresholds(value *[]float64) (*[]float64, error) {
	if value == nil {
		return nil, nil //nolint:nilnil // An absent value is not an error.
	}

	sorted, err := config.NormalizeThresholds(*value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPatch, err)
	}

	return &sorted, nil
}

// restore puts back the settings file content from before a change.
//
// Parameters:
//   - path: the settings file.
//   - previous: the earlier content.
//
// Returns:
//   - error: the file error, or nil.
func restore(path string, previous []byte) error {
	err := config.SaveFile(path, previous)
	if err != nil {
		return fmt.Errorf("restore the settings file: %w", err)
	}

	return nil
}

// removeNew removes a settings file that a rejected change created.
//
// Parameters:
//   - path: the settings file.
//
// Returns:
//   - error: the file error, or nil.
func removeNew(path string) error {
	err := os.Remove(path)
	if err != nil {
		return fmt.Errorf("remove the settings file: %w", err)
	}

	return nil
}
