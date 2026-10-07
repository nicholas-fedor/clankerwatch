// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package usage models Claude subscription usage limits and fetches them from
// the Anthropic usage endpoint.
//
// Parse turns the endpoint's payload into Bars in the order Claude Code's
// /usage view shows them. The payload is undocumented and keeps gaining keys,
// so the server's limits list is authoritative, the older flat windows fill
// in when it is missing, and unknown keys are ignored.
//
// Client sends one request per Fetch with an honest User-Agent and never
// follows redirects, so the bearer token cannot leave api.anthropic.com.
// Failures are classified as a FetchError whose Kind decides how long the
// caller backs off.
package usage
