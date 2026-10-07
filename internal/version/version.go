// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package version

import "runtime/debug"

// Info is the build metadata for this binary.
type Info struct {
	// Version is the release version, or the module version for a dev build.
	Version string

	// CommitSHA is the source revision stamped at link time.
	CommitSHA string

	// BuildTime is the UTC timestamp stamped at link time.
	BuildTime string
}

const (
	// devVersion is the unstamped Version value.
	devVersion = "dev"

	// unknown is the unstamped revision and build time.
	unknown = "unknown"
)

var (
	// Version is the application version injected at link time.
	//
	// A value of dev means the binary was not stamped. GetVersion reports the
	// module version in that case and does not assign it back to Version.
	Version = devVersion

	// CommitSHA is the git revision injected at link time.
	CommitSHA = unknown

	// BuildTime is the build timestamp injected at link time.
	BuildTime = unknown
)

// Current returns the build metadata for this binary.
//
// The Version field is the result of GetVersion, so a development build
// reports the module version without mutating the Version variable.
//
// Returns:
//   - Info: build metadata snapshot.
func Current() Info {
	return Info{
		Version:   GetVersion(),
		CommitSHA: CommitSHA,
		BuildTime: BuildTime,
	}
}

// GetVersion returns the current version string.
//
// When Version is dev, the module version from build info is returned and
// Version is left unchanged. A non-dev Version is returned as stored.
//
// Returns:
//   - string: stamped version, or the module version for a dev build.
func GetVersion() string {
	if Version != devVersion {
		return Version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}

	return info.Main.Version
}
