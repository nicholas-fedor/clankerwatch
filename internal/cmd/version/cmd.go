// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package version

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
)

// NewCommand returns the version command.
//
// Parameters:
//   - stdout: stream the version line is written to.
//   - version: the version line, such as "v0.1.0 (abc1234, 2026-10-06T12:00:00Z)".
//
// Returns:
//   - *cobra.Command: the version command.
func NewCommand(stdout io.Writer, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Long: `Print the version, commit, and build time of the installed binary.

The values are stamped into the binary at link time. A build that was never
stamped reports Go's module version and "unknown" for the rest.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return options.WriteString(stdout, options.Name+" "+version+"\n")
		},
	}
}
