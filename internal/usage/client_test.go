// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seenRequest is the part of a request a test server records.
type seenRequest struct {
	header http.Header
	method string
	path   string
}

// testVersion is the daemon version the test clients report.
const testVersion = "1.2.3"

// errTransport is the cause a test transport reports.
var errTransport = errors.New("transport failed")

// failingTransport fails every round trip with a fixed error.
type failingTransport struct{}

// RoundTrip returns errTransport.
//
// Returns:
//   - *http.Response: always nil.
//   - error: errTransport.
func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errTransport
}

// testClient returns a client for server with a fixed clock.
//
// Parameters:
//   - t: test handle.
//   - server: the local server.
//   - now: the clock the client reads.
//
// Returns:
//   - *Client: the client.
func testClient(t *testing.T, server *httptest.Server, now time.Time) *Client {
	t.Helper()

	client := NewClientWithBase(server.URL, server.Client(), testVersion)
	client.now = func() time.Time { return now }

	return client
}

// respond returns a handler that writes a fixed response.
//
// Parameters:
//   - status: the HTTP status.
//   - header: extra response headers.
//   - body: the response body.
//
// Returns:
//   - http.HandlerFunc: the handler.
func respond(status int, header map[string]string, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for key, value := range header {
			w.Header().Set(key, value)
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// requireFetchError asserts err is a *FetchError and returns it.
//
// Parameters:
//   - t: test handle.
//   - err: the error from Fetch.
//
// Returns:
//   - *FetchError: the typed error.
func requireFetchError(t *testing.T, err error) *FetchError {
	t.Helper()

	var fetchErr *FetchError

	require.ErrorAs(t, err, &fetchErr)

	return fetchErr
}

// TestFetchSendsRequest checks the method, path, and headers of a request.
//
// The token must go only in the Authorization header, and the User-Agent must
// name this daemon so requests never pose as Claude Code.
func TestFetchSendsRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan seenRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- seenRequest{header: r.Header.Clone(), method: r.Method, path: r.URL.Path}

		_, _ = w.Write([]byte(`{"limits":[]}`))
	}))
	t.Cleanup(server.Close)

	body, err := testClient(t, server, time.Now()).Fetch(t.Context(), "secret-token")
	require.NoError(t, err)
	assert.JSONEq(t, `{"limits":[]}`, string(body))

	req := <-requests
	assert.Equal(t, http.MethodGet, req.method)
	assert.Equal(t, "/api/oauth/usage", req.path)
	assert.Equal(t, "Bearer secret-token", req.header.Get("Authorization"))
	assert.Equal(t, "oauth-2025-04-20", req.header.Get("Anthropic-Beta"))
	assert.Equal(t, "application/json", req.header.Get("Accept"))
	assert.Equal(t, "clankerwatch/"+testVersion, req.header.Get("User-Agent"))
}

// TestFetchReturnsFixture passes the body of a 200 through unchanged.
func TestFetchReturnsFixture(t *testing.T) {
	t.Parallel()

	fixture := readFixture(t, liveFixture)
	server := httptest.NewServer(respond(http.StatusOK, nil, string(fixture)))
	t.Cleanup(server.Close)

	body, err := testClient(t, server, time.Now()).Fetch(t.Context(), "token")
	require.NoError(t, err)
	assert.Equal(t, fixture, body)
}

// TestFetchClassifiesStatus maps each failure status to its kind.
//
// The kind decides the backoff, so a scope error must not be mistaken for
// rate limiting, and only rate limiting may honor Retry-After.
func TestFetchClassifiesStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	retryDate := now.Add(2 * time.Minute).Format(http.TimeFormat)

	tests := []struct {
		header     map[string]string
		name       string
		body       string
		wantDetail string
		status     int
		wantRetry  time.Duration
		wantKind   ErrKind
	}{
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			body:       `{"type":"error","error":{"type":"authentication_error","message":"invalid token"}}`,
			wantKind:   KindUnauthorized,
			wantDetail: "authentication_error: invalid token",
		},
		{
			name:       "missing scope",
			status:     http.StatusForbidden,
			header:     map[string]string{"Retry-After": "60"},
			body:       `{"error":{"type":"permission_error","message":"needs user:profile"}}`,
			wantKind:   KindScope,
			wantDetail: "permission_error: needs user:profile",
		},
		{
			name:       "forbidden without scope error",
			status:     http.StatusForbidden,
			header:     map[string]string{"Retry-After": "60"},
			body:       `{"error":{"type":"forbidden","message":"slow down"}}`,
			wantKind:   KindRateLimited,
			wantDetail: "forbidden: slow down",
			wantRetry:  time.Minute,
		},
		{
			name:       "too many requests in seconds",
			status:     http.StatusTooManyRequests,
			header:     map[string]string{"Retry-After": "30"},
			body:       `{"error":{"type":"rate_limit_error","message":"rate limited"}}`,
			wantKind:   KindRateLimited,
			wantDetail: "rate_limit_error: rate limited",
			wantRetry:  30 * time.Second,
		},
		{
			name:      "too many requests with a date",
			status:    http.StatusTooManyRequests,
			header:    map[string]string{"Retry-After": retryDate},
			wantKind:  KindRateLimited,
			wantRetry: 2 * time.Minute,
		},
		{
			name:     "too many requests without a delay",
			status:   http.StatusTooManyRequests,
			body:     `not json`,
			wantKind: KindRateLimited,
		},
		{
			name:       "server error ignores Retry-After",
			status:     http.StatusInternalServerError,
			header:     map[string]string{"Retry-After": "30"},
			body:       `{"type":"api_error","message":"boom"}`,
			wantKind:   KindServer,
			wantDetail: "api_error: boom",
		},
		{
			name:     "redirect is not followed",
			status:   http.StatusFound,
			header:   map[string]string{"Location": "https://elsewhere.example/steal"},
			wantKind: KindServer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(respond(tt.status, tt.header, tt.body))
			t.Cleanup(server.Close)

			body, err := testClient(t, server, now).Fetch(t.Context(), "token")
			assert.Nil(t, body)

			fetchErr := requireFetchError(t, err)
			assert.Equal(t, tt.wantKind, fetchErr.Kind)
			assert.Equal(t, tt.status, fetchErr.Status)
			assert.Equal(t, tt.wantDetail, fetchErr.Detail)
			assert.Equal(t, tt.wantRetry, fetchErr.RetryAfter)
			require.NoError(t, fetchErr.Unwrap())
		})
	}
}

// TestFetchRejectsOversizedBody refuses a body past the size bound.
//
// The bound keeps a broken or hostile server from making the daemon buffer an
// unbounded response.
func TestFetchRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(respond(http.StatusOK, nil, strings.Repeat("x", maxBody+1)))
	t.Cleanup(server.Close)

	body, err := testClient(t, server, time.Now()).Fetch(t.Context(), "token")
	assert.Nil(t, body)

	fetchErr := requireFetchError(t, err)
	assert.Equal(t, KindServer, fetchErr.Kind)
	assert.Equal(t, http.StatusOK, fetchErr.Status)
	assert.Equal(t, "response body too large", fetchErr.Detail)
}

// TestFetchAcceptsBodyAtLimit accepts a body of exactly the size bound.
func TestFetchAcceptsBodyAtLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(respond(http.StatusOK, nil, strings.Repeat("x", maxBody)))
	t.Cleanup(server.Close)

	body, err := testClient(t, server, time.Now()).Fetch(t.Context(), "token")
	require.NoError(t, err)
	assert.Len(t, body, maxBody)
}

// TestFetchTruncatedBody reports a body cut short as a network failure.
func TestFetchTruncatedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(100))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"limits":`))
	}))
	t.Cleanup(server.Close)

	_, err := testClient(t, server, time.Now()).Fetch(t.Context(), "token")

	fetchErr := requireFetchError(t, err)
	assert.Equal(t, KindNetwork, fetchErr.Kind)
	assert.Equal(t, http.StatusOK, fetchErr.Status)
	require.Error(t, fetchErr.Unwrap())
}

// TestFetchTransportError reports a failed round trip as a network failure.
func TestFetchTransportError(t *testing.T) {
	t.Parallel()

	client := NewClientWithBase("http://127.0.0.1", &http.Client{Transport: failingTransport{}}, testVersion)

	_, err := client.Fetch(t.Context(), "token")
	require.ErrorIs(t, err, errTransport)

	fetchErr := requireFetchError(t, err)
	assert.Equal(t, KindNetwork, fetchErr.Kind)
	assert.Zero(t, fetchErr.Status)
}

// TestFetchCanceledContext stops before the request completes.
func TestFetchCanceledContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(respond(http.StatusOK, nil, `{}`))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := testClient(t, server, time.Now()).Fetch(ctx, "token")
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, KindNetwork, requireFetchError(t, err).Kind)
}

// TestFetchInvalidBase reports a base URL that cannot form a request.
func TestFetchInvalidBase(t *testing.T) {
	t.Parallel()

	client := NewClientWithBase("http://bad host\x7f", &http.Client{Transport: failingTransport{}}, testVersion)

	_, err := client.Fetch(t.Context(), "token")
	require.NotErrorIs(t, err, errTransport)
	assert.Equal(t, KindNetwork, requireFetchError(t, err).Kind)
}

// TestNewClient points at the Anthropic API with bounded timeouts.
func TestNewClient(t *testing.T) {
	t.Parallel()

	client := NewClient(testVersion)
	assert.Equal(t, DefaultBaseURL, client.base)
	assert.Equal(t, "clankerwatch/"+testVersion, client.userAgent)
	assert.NotNil(t, client.now)
	require.NotNil(t, client.httpClient.CheckRedirect)
	assert.Equal(t, requestTimeout, client.httpClient.Timeout)
}

// TestNewClientWithBaseTrimsSlash avoids a double slash before the path.
func TestNewClientWithBaseTrimsSlash(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{}
	client := NewClientWithBase("https://api.example.com/", httpClient, "dev")

	assert.Equal(t, "https://api.example.com", client.base)
	assert.Same(t, httpClient, client.httpClient)
	assert.NotNil(t, httpClient.CheckRedirect, "the redirect policy is replaced")
}

// TestNewHTTPClient checks the transport settings.
//
// Keep-alives are off because requests are minutes apart, and every phase of
// a request is bounded so a stalled server cannot hang a poll.
func TestNewHTTPClient(t *testing.T) {
	t.Parallel()

	client := newHTTPClient()
	assert.Equal(t, requestTimeout, client.Timeout)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "transport is %T", client.Transport)
	assert.True(t, transport.DisableKeepAlives)
	assert.True(t, transport.ForceAttemptHTTP2)
	assert.Equal(t, tlsTimeout, transport.TLSHandshakeTimeout)
	assert.Equal(t, requestTimeout, transport.ResponseHeaderTimeout)
	assert.NotNil(t, transport.Proxy)
	assert.NotNil(t, transport.DialContext)
}

// TestRefuseRedirect stops at the first redirect response.
func TestRefuseRedirect(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, refuseRedirect(nil, nil), http.ErrUseLastResponse)
}

// TestErrKindString names every kind and numbers unknown ones.
func TestErrKindString(t *testing.T) {
	t.Parallel()

	tests := map[ErrKind]string{
		KindNetwork:      "network error",
		KindRateLimited:  "rate limited",
		KindUnauthorized: "unauthorized",
		KindScope:        "missing scope",
		KindServer:       "server error",
		ErrKind(0):       "error 0",
		ErrKind(42):      "error 42",
	}

	for kind, want := range tests {
		assert.Equal(t, want, kind.String(), "kind %d", uint8(kind))
	}
}

// TestFetchErrorError includes only the parts that are present.
func TestFetchErrorError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  *FetchError
		name string
		want string
	}{
		{
			name: "kind only",
			err:  &FetchError{Kind: KindServer},
			want: "usage fetch: server error",
		},
		{
			name: "status and detail",
			err:  &FetchError{Kind: KindRateLimited, Status: 429, Detail: "rate_limit_error: slow"},
			want: "usage fetch: rate limited (HTTP 429): rate_limit_error: slow",
		},
		{
			name: "cause",
			err:  &FetchError{Kind: KindNetwork, Err: errTransport},
			want: "usage fetch: network error: transport failed",
		},
		{
			name: "everything",
			err:  &FetchError{Kind: KindNetwork, Status: 200, Detail: "d", Err: errTransport},
			want: "usage fetch: network error (HTTP 200): d: transport failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.EqualError(t, tt.err, tt.want)
		})
	}
}

// TestAPIError extracts the error type and a short detail.
func TestAPIError(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", maxDetail+10)

	tests := []struct {
		name       string
		body       string
		wantType   string
		wantDetail string
	}{
		{name: "not JSON", body: `<html>`},
		{name: "empty object", body: `{}`},
		{
			name:     "nested",
			body:     `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
			wantType: "rate_limit_error", wantDetail: "rate_limit_error: slow down",
		},
		{
			name:     "flat",
			body:     `{"type":"api_error","message":"boom"}`,
			wantType: "api_error", wantDetail: "api_error: boom",
		},
		{name: "type only", body: `{"type":"overloaded_error"}`, wantType: "overloaded_error", wantDetail: "overloaded_error"},
		{name: "message only", body: `{"message":"just text"}`, wantDetail: "just text"},
		{
			name:     "truncated by runes",
			body:     `{"message":"` + long + `"}`,
			wantType: "", wantDetail: strings.Repeat("é", maxDetail) + "…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			errType, detail := apiError([]byte(tt.body))
			assert.Equal(t, tt.wantType, errType)
			assert.Equal(t, tt.wantDetail, detail)
		})
	}
}
