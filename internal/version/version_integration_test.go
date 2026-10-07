// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package version_test exercises build metadata through public APIs.
package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/clankerwatch/internal/version"
)

// TestStampedReleaseBuild reports what -ldflags injected, as a release
// binary's version command does.
//
// It assigns the package globals, so it cannot run in parallel.
func TestStampedReleaseBuild(t *testing.T) {
	previousVersion, previousSHA, previousTime := version.Version, version.CommitSHA, version.BuildTime

	t.Cleanup(func() {
		version.Version, version.CommitSHA, version.BuildTime = previousVersion, previousSHA, previousTime
	})

	version.Version = "v2.0.0"
	version.CommitSHA = "0123456789abcdef"
	version.BuildTime = "2026-10-06T00:00:00Z"

	info := version.Current()
	assert.Equal(t, "v2.0.0", info.Version)
	assert.Equal(t, "0123456789abcdef", info.CommitSHA)
	assert.Equal(t, "2026-10-06T00:00:00Z", info.BuildTime)
	assert.Equal(t, info.Version, version.GetVersion())
}

// TestUnstampedBuildIsStable returns the same snapshot on repeated calls.
//
// GetVersion must not assign to Version, so a dev build reports the same
// version however many times it is asked.
func TestUnstampedBuildIsStable(t *testing.T) {
	previous := version.Version

	t.Cleanup(func() { version.Version = previous })

	version.Version = "dev"

	first := version.Current()
	second := version.Current()

	assert.Equal(t, first, second)
	assert.Equal(t, "dev", version.Version)
	assert.Equal(t, first.Version, version.GetVersion())
}
