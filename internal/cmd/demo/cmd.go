// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package demo

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// Demoer replays canned usage states.
type Demoer interface {
	// Demo serves canned states until ctx is canceled.
	//
	// Parameters:
	//   - ctx: cancellation, normally from a signal.
	//   - cfg: the settings, whose thresholds are kept.
	//
	// Returns:
	//   - error: a connection or export error.
	Demo(ctx context.Context, cfg config.Config) error
}

// NewCommand returns the demo command.
//
// Parameters:
//   - ctx: cancellation for the demo.
//   - demoer: replays the states.
//   - resolve: resolves the settings.
//
// Returns:
//   - *cobra.Command: the demo command.
func NewCommand(ctx context.Context, demoer Demoer, resolve options.Resolve) *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Replay canned usage states for widget development",
		Long: `Serve canned usage on the D-Bus session bus, cycling through every state the
widget shows: normal, warning, critical, extra usage, rate limited, paused,
and signed out, about every 12 seconds.

The real engine runs on scripted data, so alerts fire as they would for real.
Nothing reads Claude Code's files or uses the network. Stop the service first,
because only one process can own the bus name.`,
		Example: `# Replay states without sending desktop notifications.
systemctl --user stop clankerwatch
clankerwatch demo --notify "" --notify-auth=false`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}

			err = demoer.Demo(ctx, cfg)
			if err != nil {
				return fmt.Errorf("demo: %w", err)
			}

			return nil
		},
	}
}
