// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package main is the clankerwatch process entrypoint.
//
// It owns the only process exit. Signal cancellation is passed to app.Run.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nicholas-fedor/clankerwatch/internal/app"
)

// main runs clankerwatch and exits with the status from app.Run.
//
// The process context is canceled on interrupt or SIGTERM. stop runs before
// [os.Exit] so the signal registration is released.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	code := app.Run(ctx)

	stop()
	os.Exit(code)
}
