// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// TestNewReport checks every field comes from its source.
func TestNewReport(t *testing.T) {
	t.Parallel()

	cfg := config.Default(config.Dirs{Home: "/home/tester", StateHome: "/state"})
	cfg.Mode = config.ModeCacheOnly
	cfg.Thresholds = []float64{50}
	cfg.NotifyAuth = false

	paths := claudecode.Paths{
		Dir:          "/claude",
		Credentials:  "/claude/.credentials.json",
		GlobalConfig: "/claude/.claude.json",
		Sessions:     "/claude/sessions",
	}

	report := newReport("v1.2.3", cfg, paths, `{"status":"no_data"}`)

	assert.Equal(t, Report{
		Version:      "v1.2.3",
		Mode:         config.ModeCacheOnly,
		Credentials:  "/claude/.credentials.json",
		GlobalConfig: "/claude/.claude.json",
		Sessions:     "/claude/sessions",
		StateFile:    "/state/clankerwatch/state.json",
		Snapshot:     json.RawMessage(`{"status":"no_data"}`),
		Thresholds:   []float64{50},
		Interval:     config.DefaultInterval,
		IdleInterval: config.DefaultIdleInterval,
		NotifyAuth:   false,
	}, report)
}

// TestReportJSONKeys checks the field names scripts depend on.
func TestReportJSONKeys(t *testing.T) {
	t.Parallel()

	report := newReport("v1", config.Default(config.Dirs{Home: "/h", StateHome: "/s"}),
		claudecode.ResolvePaths("", "/h"), `{"v":1}`)

	content, err := json.Marshal(report)
	require.NoError(t, err)

	var keys map[string]json.RawMessage

	require.NoError(t, json.Unmarshal(content, &keys))

	for _, key := range []string{
		"version", "mode", "credentials", "globalConfig", "sessions", "stateFile",
		"snapshot", "thresholds", "interval", "idleInterval", "notifyAuth",
	} {
		assert.Contains(t, keys, key)
	}

	assert.JSONEq(t, `{"v":1}`, string(keys["snapshot"]))
}
