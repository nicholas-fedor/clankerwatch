// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/demo"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/notifytest"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/serve"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/status"
	"github.com/nicholas-fedor/clankerwatch/internal/cmd/version"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// NewRoot returns the clankerwatch command tree wired to deps.
//
// SilenceUsage and SilenceErrors are set so the caller owns usage and error
// text. Every command uses RunE and returns errors instead of exiting.
//
// Parameters:
//   - ctx: cancellation for every subcommand.
//   - deps: process dependencies.
//
// Returns:
//   - *cobra.Command: the root command.
func NewRoot(ctx context.Context, deps Dependencies) *cobra.Command {
	opts := options.New()
	root := &cobra.Command{
		Use:   options.Name,
		Short: "Claude Code subscription usage for the KDE Plasma panel",
		Long: `clankerwatch publishes Claude Code subscription usage on the D-Bus session
bus for a KDE Plasma widget: the current 5-hour session, the weekly limits,
their reset times, and extra usage.

It reads Claude Code's login without ever refreshing or writing it, calls the
Anthropic usage endpoint at most every five minutes while Claude Code is
active, and adopts Claude Code's own cached result whenever that is newer.
Desktop notifications fire when usage crosses the alert thresholds.

Settings come from ~/.config/clankerwatch/config.yaml. An environment
variable overrides the file, and a flag overrides both.`,
		Example: `# Run the daemon, as the systemd user unit does.
clankerwatch serve

# Print the settings and the snapshot the daemon would publish, offline.
clankerwatch status

# Replay canned states for widget development.
clankerwatch demo --notify ""`,
		Args: cobra.NoArgs,
		RunE: runHelp,
	}

	root.SilenceUsage = true
	root.SilenceErrors = true

	options.BindPersistent(root, opts)

	resolve := func() (config.Config, error) {
		cfg, err := options.ResolveSettings(root.PersistentFlags(), lookupEnv(deps), deps.Dirs)
		if err != nil {
			return config.Config{}, fmt.Errorf("resolve settings: %w", err)
		}

		if deps.Levels != nil {
			deps.Levels.SetLevel(cfg.LogLevel)
		}

		return cfg, nil
	}

	root.AddCommand(serve.NewCommand(ctx, deps.Serve, resolve))
	root.AddCommand(status.NewCommand(ctx, deps.Status, resolve, deps.Stdout))
	root.AddCommand(demo.NewCommand(ctx, deps.Demo, resolve))
	root.AddCommand(notifytest.NewCommand(ctx, deps.NotifyTest))
	root.AddCommand(version.NewCommand(deps.Stdout, deps.Version))

	return root
}

// DocRoot returns the command tree for documentation generation.
//
// tools/docgen reads command metadata only, so the tree carries no
// dependencies and no command is executed.
//
// Parameters:
//   - ctx: context for the tree. The generator never executes it.
//
// Returns:
//   - *cobra.Command: the root command, safe to introspect.
func DocRoot(ctx context.Context) *cobra.Command {
	return NewRoot(ctx, Dependencies{
		Serve: nil, Demo: nil, Status: nil, NotifyTest: nil, Levels: nil,
		Stdout: nil, LookupEnv: nil, Dirs: config.Dirs{Home: "", StateHome: "", ConfigHome: ""}, Version: "",
	})
}

// lookupEnv returns the environment reader, defaulting to [os.LookupEnv].
//
// Parameters:
//   - deps: process dependencies.
//
// Returns:
//   - func: the environment reader.
func lookupEnv(deps Dependencies) func(string) (string, bool) {
	if deps.LookupEnv != nil {
		return deps.LookupEnv
	}

	return os.LookupEnv
}

// runHelp writes command help when no subcommand is given.
//
// Parameters:
//   - command: root command whose help text is written.
//   - args: unused positional arguments.
//
// Returns:
//   - error: non-nil when help cannot be written.
func runHelp(command *cobra.Command, _ []string) error {
	err := command.Help()
	if err != nil {
		return fmt.Errorf("%w: %w", options.ErrWriteOutput, err)
	}

	return nil
}
