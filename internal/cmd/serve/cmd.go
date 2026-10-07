// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package serve

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// Server runs the daemon.
type Server interface {
	// Serve runs until ctx is canceled.
	//
	// Parameters:
	//   - ctx: cancellation, normally from a signal.
	//   - cfg: the settings.
	//   - reload: resolves the settings again after the settings file changes.
	//
	// Returns:
	//   - error: a connection or export error.
	Serve(ctx context.Context, cfg config.Config, reload func() (config.Config, error)) error
}

// NewCommand returns the serve command.
//
// Parameters:
//   - ctx: cancellation for the daemon.
//   - server: runs the daemon.
//   - resolve: resolves the settings.
//
// Returns:
//   - *cobra.Command: the serve command.
func NewCommand(ctx context.Context, server Server, resolve options.Resolve) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the daemon",
		Long: `Run the daemon on the D-Bus session bus until it receives SIGINT or SIGTERM.

D-Bus normally starts it on demand through the systemd user unit when the
widget loads, so running it by hand is only needed for debugging. Only one
instance can own the bus name at a time.

Changes from the widget's settings page apply at once. After editing the
settings file by hand, send SIGHUP to apply it, for example with
systemctl --user reload clankerwatch.`,
		Example: `# Run in the foreground with debug logging.
clankerwatch serve --log-level debug`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}

			err = server.Serve(ctx, cfg, resolve)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}

			return nil
		},
	}
}
