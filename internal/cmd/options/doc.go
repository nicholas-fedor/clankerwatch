// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package options binds the settings flags shared by every command and
// resolves them into a config.Config.
//
// A setting comes from its flag when the flag was given, otherwise from its
// environment variable when that is set, otherwise from the default. Every
// resolution error wraps ErrInvalidSetting, which the process maps to exit
// status 2.
package options
