// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package notifytest

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Sender sends a sample notification.
type Sender interface {
	// NotifyTest sends one sample notification.
	//
	// Parameters:
	//   - ctx: cancellation for the call.
	//
	// Returns:
	//   - error: a connection or send error.
	NotifyTest(ctx context.Context) error
}

// NewCommand returns the notify-test command.
//
// Parameters:
//   - ctx: cancellation for the call.
//   - sender: sends the notification.
//
// Returns:
//   - *cobra.Command: the notify-test command.
func NewCommand(ctx context.Context, sender Sender) *cobra.Command {
	return &cobra.Command{
		Use:   "notify-test",
		Short: "Send a sample desktop notification",
		Long: `Send one sample notification through org.freedesktop.Notifications, so you
can check how usage alerts look and that your notification settings let them
through.`,
		Example: `clankerwatch notify-test`,
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			err := sender.NotifyTest(ctx)
			if err != nil {
				return fmt.Errorf("notify-test: %w", err)
			}

			return nil
		},
	}
}
