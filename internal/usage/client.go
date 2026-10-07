// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrKind classifies a failed fetch. The kind decides how long the caller
// backs off.
type ErrKind uint8

// FetchError describes why a usage fetch failed.
type FetchError struct {
	// Err is the underlying transport error.
	Err error

	// Detail is the API error type and message, truncated.
	Detail string

	// Status is the HTTP status, or 0 for a transport error.
	Status int

	// RetryAfter is the delay the server asked for, or 0 when it did not.
	RetryAfter time.Duration

	// Kind classifies the failure.
	Kind ErrKind
}

// Client fetches the usage payload for one OAuth token.
type Client struct {
	httpClient *http.Client
	now        func() time.Time
	base       string
	userAgent  string
}

// apiErrorBody is the error shape the API returns, nested or flat.
type apiErrorBody struct {
	Error   *apiErrorBody `json:"error"`
	Type    string        `json:"type"`
	Message string        `json:"message"`
}

// Failure kinds.
const (
	// KindNetwork is a transport failure, including timeouts.
	KindNetwork ErrKind = iota + 1

	// KindRateLimited is a 429, or a 403 that is not a scope error.
	KindRateLimited

	// KindUnauthorized is a rejected token.
	KindUnauthorized

	// KindScope is a token without the user:profile scope.
	KindScope

	// KindServer is any other failure, including an oversized body.
	KindServer
)

const (
	// DefaultBaseURL is the Anthropic API.
	DefaultBaseURL = "https://api.anthropic.com"

	// usagePath is the subscription usage endpoint.
	usagePath = "/api/oauth/usage"

	// betaHeader enables OAuth access to the endpoint.
	betaHeader = "oauth-2025-04-20"

	// maxBody bounds the accepted response size.
	maxBody = 1 << 20

	// maxDetail bounds the error detail kept from a response.
	maxDetail = 200

	// permissionError is the API error type of a missing scope.
	permissionError = "permission_error"

	// requestTimeout bounds a whole request.
	requestTimeout = 15 * time.Second

	// dialTimeout bounds the TCP connect.
	dialTimeout = 5 * time.Second

	// tlsTimeout bounds the TLS handshake.
	tlsTimeout = 10 * time.Second
)

// NewClient returns a client for the Anthropic usage endpoint.
//
// The User-Agent names this daemon and its version, so requests never pose as
// Claude Code.
//
// Parameters:
//   - version: the daemon version for the User-Agent.
//
// Returns:
//   - *Client: a client for DefaultBaseURL.
func NewClient(version string) *Client {
	return NewClientWithBase(DefaultBaseURL, newHTTPClient(), version)
}

// NewClientWithBase returns a client for another base URL and HTTP client.
//
// Tests point it at a local server. The daemon always uses NewClient.
//
// Parameters:
//   - base: the API root, without a trailing slash.
//   - httpClient: the HTTP client. Its redirect policy is replaced.
//   - version: the daemon version for the User-Agent.
//
// Returns:
//   - *Client: the client.
func NewClientWithBase(base string, httpClient *http.Client, version string) *Client {
	httpClient.CheckRedirect = refuseRedirect

	return &Client{
		base:       strings.TrimSuffix(base, "/"),
		httpClient: httpClient,
		userAgent:  "clankerwatch/" + version,
		now:        time.Now,
	}
}

// Error describes the failure.
//
// Returns:
//   - string: the kind, status, detail, and cause.
func (e *FetchError) Error() string {
	text := "usage fetch: " + e.Kind.String()
	if e.Status != 0 {
		text += " (HTTP " + strconv.Itoa(e.Status) + ")"
	}

	if e.Detail != "" {
		text += ": " + e.Detail
	}

	if e.Err != nil {
		text += ": " + e.Err.Error()
	}

	return text
}

// Unwrap returns the underlying transport error.
//
// Returns:
//   - error: the cause, or nil.
func (e *FetchError) Unwrap() error { return e.Err }

// Fetch requests the usage payload.
//
// Parameters:
//   - ctx: cancellation and deadline for the request.
//   - bearer: the OAuth access token.
//
// Returns:
//   - []byte: the payload on HTTP 200.
//   - error: a *FetchError for every failure.
func (c *Client) Fetch(ctx context.Context, bearer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+usagePath, http.NoBody)
	if err != nil {
		return nil, &FetchError{Kind: KindNetwork, Status: 0, RetryAfter: 0, Detail: "", Err: err}
	}

	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Anthropic-Beta", betaHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &FetchError{Kind: KindNetwork, Status: 0, RetryAfter: 0, Detail: "", Err: err}
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, &FetchError{Kind: KindNetwork, Status: resp.StatusCode, RetryAfter: 0, Detail: "", Err: err}
	}

	if len(body) > maxBody {
		return nil, &FetchError{
			Kind: KindServer, Status: resp.StatusCode, RetryAfter: 0, Detail: "response body too large", Err: nil,
		}
	}

	if resp.StatusCode == http.StatusOK {
		return body, nil
	}

	return nil, c.classify(resp, body)
}

// String names the failure kind.
//
// Returns:
//   - string: a short description.
func (k ErrKind) String() string {
	switch k {
	case KindNetwork:
		return "network error"
	case KindRateLimited:
		return "rate limited"
	case KindUnauthorized:
		return "unauthorized"
	case KindScope:
		return "missing scope"
	case KindServer:
		return "server error"
	default:
		return "error " + strconv.Itoa(int(k))
	}
}

// classify turns a non-200 response into a FetchError.
//
// Parameters:
//   - resp: the response.
//   - body: the response body.
//
// Returns:
//   - *FetchError: the classified failure.
func (c *Client) classify(resp *http.Response, body []byte) *FetchError {
	errType, detail := apiError(body)
	fetchErr := &FetchError{Kind: KindServer, Status: resp.StatusCode, RetryAfter: 0, Detail: detail, Err: nil}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		fetchErr.Kind = KindUnauthorized
	case http.StatusForbidden:
		// Claude Code treats a 403 that is not a scope error as rate limiting.
		fetchErr.Kind = KindRateLimited
		if errType == permissionError {
			fetchErr.Kind = KindScope
		}
	case http.StatusTooManyRequests:
		fetchErr.Kind = KindRateLimited
	default:
	}

	if fetchErr.Kind == KindRateLimited {
		fetchErr.RetryAfter, _ = ParseRetryAfter(resp.Header.Get("Retry-After"), c.now())
	}

	return fetchErr
}

// newHTTPClient returns the HTTP client for the usage endpoint.
//
// Keep-alives are off because requests are minutes apart.
//
// Returns:
//   - *[http.Client]: the client.
func newHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout} //nolint:exhaustruct_v5 // Zero values are the defaults.

	//nolint:exhaustruct_v5 // Zero values are the defaults.
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: requestTimeout,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
	}

	return &http.Client{Transport: transport, Timeout: requestTimeout}
}

// refuseRedirect keeps the token from following a redirect to another host.
//
// Returns:
//   - error: [http.ErrUseLastResponse], so the redirect response is returned.
func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// apiError extracts the error type and a short detail from an API error body.
//
// Parameters:
//   - body: the response body.
//
// Returns:
//   - errType: the API error type, such as rate_limit_error.
//   - detail: "type: message", truncated.
//
//nolint:nonamedreturns // Same-type returns need names.
func apiError(body []byte) (errType, detail string) {
	var parsed apiErrorBody

	if json.Unmarshal(body, &parsed) != nil {
		return "", ""
	}

	errType = parsed.Type

	message := parsed.Message

	if parsed.Error != nil {
		errType, message = parsed.Error.Type, parsed.Error.Message
	}

	detail = strings.TrimSpace(strings.TrimPrefix(errType+": "+message, ": "))

	detail = strings.TrimSuffix(detail, ":")

	if runes := []rune(detail); len(runes) > maxDetail {
		detail = string(runes[:maxDetail]) + "…"
	}

	return errType, detail
}
