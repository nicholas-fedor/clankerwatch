// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
)

// statusText is a status and its explanation.
type statusText struct {
	status  Status
	message string
}

// statusFor maps the engine state to the published status.
//
// Claude Code's own data from after a run of failures began is current, so it
// reports ok while the backoff keeps running.
//
// Parameters:
//   - mode: the data source.
//   - gate: the current gate.
//   - backoff: the failure state.
//   - data: the current data, or nil.
//
// Returns:
//   - Status: the status.
//   - string: the explanation, empty for ok.
func statusFor(mode config.Mode, gate schedule.Gate, backoff schedule.Backoff, data *state.Data) (Status, string) {
	if mode == config.ModeCacheOnly {
		if data != nil {
			return StatusOK, ""
		}

		return StatusNoData, "Claude Code has not cached any usage yet"
	}

	if text, ok := loginStatus(gate); ok {
		return text.status, text.message
	}

	if gate == schedule.GateBackoff {
		if data != nil && data.Source == sourceClaudeCode && data.FetchedAt.After(backoff.Since) {
			return StatusOK, ""
		}

		text := backoffStatus(backoff.Kind)

		return text.status, text.message
	}

	if data != nil {
		return StatusOK, ""
	}

	return StatusStarting, ""
}

// loginStatus maps the gates that wait on the login.
//
// Parameters:
//   - gate: the current gate.
//
// Returns:
//   - statusText: the status.
//   - bool: false for gates that do not concern the login.
func loginStatus(gate schedule.Gate) (statusText, bool) {
	switch gate {
	case schedule.GateLoggedOut:
		return statusText{StatusLoggedOut, "Claude Code is not signed in"}, true
	case schedule.GateCredentials:
		return statusText{StatusAuthError, "Claude Code's credentials cannot be read"}, true
	case schedule.GateScope:
		return statusText{StatusAuthError, "Claude Code's login lacks the user:profile scope"}, true
	case schedule.GateAuthBlocked:
		return statusText{StatusAuthError, "The usage endpoint rejected Claude Code's login"}, true
	case schedule.GateTokenExpiring:
		return statusText{StatusTokenExpired, "Waiting for Claude Code to refresh its login"}, true
	case schedule.GateNone, schedule.GateBackoff:
		return statusText{}, false
	default:
		return statusText{}, false
	}
}

// backoffStatus maps a backoff kind.
//
// Parameters:
//   - kind: the failure being waited out.
//
// Returns:
//   - statusText: the status.
func backoffStatus(kind schedule.BackoffKind) statusText {
	switch kind {
	case schedule.BackoffRateLimited:
		return statusText{StatusRateLimited, "The usage endpoint is rate limited"}
	case schedule.BackoffNetwork:
		return statusText{StatusOffline, "api.anthropic.com cannot be reached"}
	case schedule.BackoffBadPayload:
		return statusText{StatusError, "The usage endpoint returned an unrecognized payload"}
	case schedule.BackoffServer:
		return statusText{StatusError, "The usage endpoint returned an error"}
	default:
		return statusText{StatusError, "The usage endpoint returned an error"}
	}
}
