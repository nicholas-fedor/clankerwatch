// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restoreStamps puts the link-time variables back after a test.
//
// Tests in this package assign the package globals, so none of them run in
// parallel and each restores what it changed.
//
// Parameters:
//   - t: test handle.
func restoreStamps(t *testing.T) {
	t.Helper()

	previousVersion, previousSHA, previousTime := Version, CommitSHA, BuildTime

	t.Cleanup(func() {
		Version, CommitSHA, BuildTime = previousVersion, previousSHA, previousTime
	})
}

// TestUnstampedDefaults checks the values a binary carries without -ldflags.
//
// Printing empty fields would look like a bug. "dev" and "unknown" tell the
// reader the binary was not stamped.
func TestUnstampedDefaults(t *testing.T) {
	assert.Equal(t, "dev", devVersion)
	assert.Equal(t, "unknown", unknown)
	assert.Equal(t, devVersion, Version)
	assert.Equal(t, unknown, CommitSHA)
	assert.Equal(t, unknown, BuildTime)
}

// TestGetVersionPrefersStampedValue checks a release build reports its stamp.
//
// The stamp is what a user quotes in a bug report, so it has to win over
// whatever the module cache happens to hold.
func TestGetVersionPrefersStampedValue(t *testing.T) {
	restoreStamps(t)

	Version = "v1.2.3"

	assert.Equal(t, "v1.2.3", GetVersion())
}

// TestGetVersionFallsBackToBuildInfo reports the module version for a dev
// build.
func TestGetVersionFallsBackToBuildInfo(t *testing.T) {
	restoreStamps(t)

	Version = devVersion

	// A test binary is a main module, so build info is always available here.
	info, ok := debug.ReadBuildInfo()
	require.True(t, ok)

	assert.Equal(t, info.Main.Version, GetVersion())
}

// TestGetVersionLeavesVersionUnchanged checks the fallback does not assign.
//
// Mutating the package variable would make the first call sticky, so a later
// stamp would be ignored.
func TestGetVersionLeavesVersionUnchanged(t *testing.T) {
	restoreStamps(t)

	Version = devVersion
	GetVersion()

	assert.Equal(t, devVersion, Version)
}

// TestCurrentCarriesStamps copies every stamped field into the snapshot.
func TestCurrentCarriesStamps(t *testing.T) {
	restoreStamps(t)

	Version = "v9.9.9"
	CommitSHA = "abc1234"
	BuildTime = "2026-01-01T00:00:00Z"

	assert.Equal(t, Info{Version: "v9.9.9", CommitSHA: "abc1234", BuildTime: "2026-01-01T00:00:00Z"}, Current())
}

// TestCurrentUsesGetVersionForDevBuilds reports the module version without
// stamping it.
func TestCurrentUsesGetVersionForDevBuilds(t *testing.T) {
	restoreStamps(t)

	Version = devVersion

	info := Current()
	assert.Equal(t, GetVersion(), info.Version)
	assert.Equal(t, unknown, info.CommitSHA)
	assert.Equal(t, unknown, info.BuildTime)
	assert.Equal(t, devVersion, Version)
}
