// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package usage_test exercises fetching and parsing usage through public APIs.
package usage_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// usageServer starts a local usage endpoint that checks the token.
//
// Parameters:
//   - t: test handle.
//   - token: the token the server accepts.
//   - body: the payload returned for that token.
//
// Returns:
//   - *httptest.Server: the running server, closed on cleanup.
func usageServer(t *testing.T, token, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/usage" {
			http.NotFound(w, r)

			return
		}

		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad token"}}`))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server
}

// liveFixture returns the captured payload.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - string: the contents of testdata/live.json.
func liveFixture(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "live.json"))
	require.NoError(t, err)

	return string(data)
}

// TestFetchAndParseLivePayload runs one poll end to end.
//
// This is the daemon's steady state: fetch with a valid token, parse, and
// grade every bar against the default thresholds.
func TestFetchAndParseLivePayload(t *testing.T) {
	t.Parallel()

	server := usageServer(t, "good-token", liveFixture(t))
	client := usage.NewClientWithBase(server.URL+"/", server.Client(), "test")

	body, err := client.Fetch(t.Context(), "good-token")
	require.NoError(t, err)

	parsed, err := usage.Parse(body)
	require.NoError(t, err)
	require.Len(t, parsed.Bars, 3)
	assert.Nil(t, parsed.Extra)

	want := []struct {
		id      string
		short   string
		percent float64
	}{
		{id: "session", short: "5h", percent: 6},
		{id: "weekly_all", short: "7d", percent: 18},
		{id: "weekly_scoped:model:fable", short: "Fable", percent: 0},
	}

	headlines := 0

	for index, bar := range parsed.Bars {
		assert.Equal(t, want[index].id, bar.ID)
		assert.Equal(t, want[index].short, bar.Short)
		assert.InDelta(t, want[index].percent, bar.Percent, 1e-9)
		assert.False(t, bar.ResetsAt.IsZero(), "bar %s has a reset time", bar.ID)
		assert.Equal(t, usage.LevelNormal, usage.LevelOf(bar, 80, 95), "bar %s", bar.ID)

		if bar.Headline {
			headlines++

			assert.Equal(t, "weekly_all", bar.ID)
		}
	}

	assert.Equal(t, 1, headlines)
}

// TestFetchWithRejectedToken reports an unauthorized fetch.
func TestFetchWithRejectedToken(t *testing.T) {
	t.Parallel()

	server := usageServer(t, "good-token", liveFixture(t))
	client := usage.NewClientWithBase(server.URL, server.Client(), "test")

	body, err := client.Fetch(t.Context(), "stale-token")
	assert.Nil(t, body)

	var fetchErr *usage.FetchError

	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, usage.KindUnauthorized, fetchErr.Kind)
	assert.Equal(t, http.StatusUnauthorized, fetchErr.Status)
	assert.Equal(t, "authentication_error: bad token", fetchErr.Detail)
	require.EqualError(t, err, "usage fetch: unauthorized (HTTP 401): authentication_error: bad token")
}

// TestFetchRateLimitedHonorsRetryAfter carries the server's delay to the
// caller and agrees with ParseRetryAfter.
func TestFetchRateLimitedHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	_, err := usage.NewClientWithBase(server.URL, server.Client(), "test").Fetch(t.Context(), "token")

	var fetchErr *usage.FetchError

	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, usage.KindRateLimited, fetchErr.Kind)
	assert.Equal(t, 2*time.Minute, fetchErr.RetryAfter)

	delay, ok := usage.ParseRetryAfter("120", time.Now())
	assert.True(t, ok)
	assert.Equal(t, fetchErr.RetryAfter, delay)
}

// TestFetchUnreachableServer reports a network failure for a closed server.
func TestFetchUnreachableServer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	base := server.URL
	server.Close()

	_, err := usage.NewClientWithBase(base, &http.Client{}, "test").Fetch(t.Context(), "token")

	var fetchErr *usage.FetchError

	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, usage.KindNetwork, fetchErr.Kind)
	require.Error(t, errors.Unwrap(err))
}

// TestParseExtraUsageEndToEnd formats pay-as-you-go amounts for display.
func TestParseExtraUsageEndToEnd(t *testing.T) {
	t.Parallel()

	server := usageServer(t, "token", `{
		"limits": [
			{"kind": "session", "percent": 97, "severity": "critical", "is_active": true},
			{"kind": "weekly_all", "percent": 82, "severity": "normal"}
		],
		"extra_usage": {
			"is_enabled": true,
			"monthly_limit": 2000,
			"used_credits": 1999,
			"utilization": 99.95,
			"currency": "GBP",
			"decimal_places": 2
		}
	}`)

	body, err := usage.NewClientWithBase(server.URL, server.Client(), "test").Fetch(t.Context(), "token")
	require.NoError(t, err)

	parsed, err := usage.Parse(body)
	require.NoError(t, err)
	require.Len(t, parsed.Bars, 2)
	assert.Equal(t, usage.LevelCritical, usage.LevelOf(parsed.Bars[0], 80, 95))
	assert.Equal(t, usage.LevelWarning, usage.LevelOf(parsed.Bars[1], 80, 95))

	require.NotNil(t, parsed.Extra)
	require.NotNil(t, parsed.Extra.Used)
	require.NotNil(t, parsed.Extra.Limit)
	require.NotNil(t, parsed.Extra.Percent)
	assert.Equal(t, "£19.99", parsed.Extra.Used.String())
	assert.Equal(t, "£20.00", parsed.Extra.Limit.String())
	assert.InDelta(t, 99.95, *parsed.Extra.Percent, 1e-9)
}

// TestParseUnrecognizedPayload rejects a response that is not usage data.
//
// A captive portal or a changed API can answer 200 with unrelated JSON, which
// must not be shown as zero usage.
func TestParseUnrecognizedPayload(t *testing.T) {
	t.Parallel()

	server := usageServer(t, "token", `{"status":"ok"}`)

	body, err := usage.NewClientWithBase(server.URL, server.Client(), "test").Fetch(t.Context(), "token")
	require.NoError(t, err)

	_, err = usage.Parse(body)
	require.ErrorIs(t, err, usage.ErrUnrecognized)
}
