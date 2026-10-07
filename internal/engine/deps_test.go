// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
)

// TestOptionsFromCarriesTheConfig checks each setting reaches the engine.
//
// The color thresholds follow the alert thresholds, so bars turn amber and
// red where alerts fire.
func TestOptionsFromCarriesTheConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Mode:         config.ModeCacheOnly,
		Interval:     3 * time.Minute,
		IdleInterval: 30 * time.Minute,
		Thresholds:   []float64{50, 75, 90},
		NotifyAuth:   true,
	}

	got := OptionsFrom(cfg)

	assert.Equal(t, Options{
		Mode:       config.ModeCacheOnly,
		Thresholds: []float64{50, 75, 90},
		Policy:     schedule.DefaultPolicy(3*time.Minute, 30*time.Minute),
		Warn:       50,
		Crit:       90,
		NotifyAuth: true,
	}, got)
}

// TestOptionsFromWithoutThresholdsUsesDefaultColors checks colors survive disabled alerts.
func TestOptionsFromWithoutThresholdsUsesDefaultColors(t *testing.T) {
	t.Parallel()

	got := OptionsFrom(config.Config{Mode: config.ModeHybrid, Interval: 5 * time.Minute, IdleInterval: 20 * time.Minute})

	assert.Empty(t, got.Thresholds)
	assert.InDelta(t, 80.0, got.Warn, 0.001)
	assert.InDelta(t, 95.0, got.Crit, 0.001)
	assert.False(t, got.NotifyAuth)
}
