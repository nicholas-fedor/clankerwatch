// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/engine/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// errBoom is a generic collaborator failure.
var errBoom = errors.New("boom")

// testNow is the wake time in these tests, on a whole second so payload
// times round-trip exactly.
var testNow = time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

// testPayload renders a usage payload with a session and a weekly bar.
//
// Parameters:
//   - tb: test or benchmark handle.
//   - session: the session percentage.
//   - weekly: the weekly percentage.
//   - resets: when the session resets. The week resets six days later.
//
// Returns:
//   - []byte: the payload JSON.
func testPayload(tb testing.TB, session, weekly float64, resets time.Time) []byte {
	tb.Helper()

	body := map[string]any{
		"limits": []map[string]any{
			{
				"kind": "session", "group": "session", "percent": session, "severity": "normal",
				"resets_at": resets.UTC().Format(time.RFC3339),
			},
			{
				"kind": "weekly_all", "group": "weekly", "percent": weekly, "severity": "normal",
				"resets_at": resets.Add(6 * 24 * time.Hour).UTC().Format(time.RFC3339), "is_active": true,
			},
		},
		"extra_usage": map[string]any{
			"is_enabled": true, "monthly_limit": 5000, "used_credits": 1250,
			"currency": "USD", "decimal_places": 2,
		},
	}

	content, err := json.Marshal(body)
	require.NoError(tb, err)

	return content
}

// testLogin returns a usable Max 5x login that expires at expiresAt.
//
// Parameters:
//   - expiresAt: the token expiry.
//
// Returns:
//   - claudecode.OAuth: the login.
func testLogin(expiresAt time.Time) claudecode.OAuth {
	return claudecode.OAuth{
		AccessToken:      "secret-token",
		ExpiresAt:        expiresAt,
		LoginExpiresAt:   expiresAt.Add(30 * 24 * time.Hour),
		Scopes:           []string{"user:profile"},
		SubscriptionType: "max",
		RateLimitTier:    "default_claude_max_5x",
	}
}

// testOptions returns hybrid-mode options with the production schedule.
//
// Returns:
//   - Options: the options.
func testOptions() Options {
	return Options{
		Mode:       config.ModeHybrid,
		Policy:     schedule.DefaultPolicy(5*time.Minute, 20*time.Minute),
		Thresholds: []float64{80, 95},
		Warn:       80,
		Crit:       95,
		NotifyAuth: true,
	}
}

// newTestEngine builds an engine, defaulting to an in-memory store.
//
// Parameters:
//   - tb: test or benchmark handle.
//   - opt: the options.
//   - deps: the collaborators.
//
// Returns:
//   - *Engine: the engine.
func newTestEngine(tb testing.TB, opt Options, deps Deps) *Engine {
	tb.Helper()

	if deps.Store == nil {
		deps.Store = new(state.Memory)
	}

	return New(tb.Context(), opt, deps)
}

// withData gives the engine parsed data fetched at fetched.
//
// Parameters:
//   - tb: test or benchmark handle.
//   - e: the engine.
//   - payload: the payload.
//   - fetched: the fetch time.
func withData(tb testing.TB, e *Engine, payload []byte, fetched time.Time) {
	tb.Helper()

	parsed, err := usage.Parse(payload)
	require.NoError(tb, err)

	e.setData(parsed, payload, fetched, sourceAPI, "account-1")
}

// TestNewDefaultsToANopLogger checks a nil logger is replaced.
//
// A nil Logger is documented as a no-op, so New must not leave a nil
// interface that would panic on the first log call.
func TestNewDefaultsToANopLogger(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, testOptions(), Deps{})

	assert.Equal(t, nopLogger{}, e.log)
	assert.NotEmpty(t, e.Snapshot())
	assert.False(t, e.dirty)
}

// TestNewWarnsAboutDiscardedState checks a load error is logged and survived.
func TestNewWarnsAboutDiscardedState(t *testing.T) {
	t.Parallel()

	store := mocks.NewMockStore(t)
	store.EXPECT().Load().Return(state.Fresh(), errBoom).Once()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Warn(mock.Anything, "Discarding saved state", mock.Anything).Once()

	e := New(t.Context(), testOptions(), Deps{Store: store, Logger: logger})

	assert.Equal(t, state.Fresh(), e.state)
}

// TestNewDropsDataTheParserRejects checks unparseable saved data is discarded.
//
// Data saved by another version can be in a shape this parser does not
// accept, and showing it half-parsed would be worse than showing nothing.
func TestNewDropsDataTheParserRejects(t *testing.T) {
	t.Parallel()

	saved := state.Fresh()
	saved.Data = &state.Data{Payload: json.RawMessage(`{"unknown":1}`), Source: sourceAPI}

	store := mocks.NewMockStore(t)
	store.EXPECT().Load().Return(saved, nil).Once()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Warn(mock.Anything, "Dropping saved data the parser no longer accepts", mock.Anything).Once()

	e := New(t.Context(), testOptions(), Deps{Store: store, Logger: logger})

	assert.Nil(t, e.state.Data)
	assert.Empty(t, e.usage.Bars)
}

// TestNewRestoresSavedData checks saved data is parsed again on start.
func TestNewRestoresSavedData(t *testing.T) {
	t.Parallel()

	now := testNow
	store := new(state.Memory)
	saved := state.Fresh()
	saved.Data = &state.Data{Payload: testPayload(t, 40, 20, now.Add(time.Hour)), Source: sourceAPI, FetchedAt: now}
	require.NoError(t, store.Save(saved))

	credentials := mocks.NewMockCredentialsSource(t)
	credentials.EXPECT().Read().Return(testLogin(time.Now().Add(8*time.Hour)), nil).Once()

	e := New(t.Context(), testOptions(), Deps{Store: store, Credentials: credentials})

	require.Len(t, e.usage.Bars, 2)
	assert.InDelta(t, 40.0, e.usage.Bars[0].Percent, 0.001)
	status, _ := e.status()
	assert.Equal(t, StatusOK, status)
}

// TestRequestRefreshCoalesces checks repeated requests never block.
func TestRequestRefreshCoalesces(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, testOptions(), Deps{})

	e.RequestRefresh()
	e.RequestRefresh()
	e.RequestRefresh()

	assert.Len(t, e.refresh, 1)
}

// TestPublishSnapshotOnlyOnChange checks the publisher sees each change once.
func TestPublishSnapshotOnlyOnChange(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, testOptions(), Deps{})

	e.publishSnapshot()

	var published []string

	e.SetPublisher(func(snap string) { published = append(published, snap) })

	e.publishSnapshot()
	assert.Empty(t, published, "an unchanged snapshot is not published")

	e.active = true
	e.publishSnapshot()
	e.publishSnapshot()

	require.Len(t, published, 1)
	assert.Equal(t, e.Snapshot(), published[0])
	assert.Contains(t, published[0], `"active":true`)
}

// TestFetchWithoutFetcherDoesNothing checks a nil fetcher records no attempt.
func TestFetchWithoutFetcherDoesNothing(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, testOptions(), Deps{})

	e.fetch(t.Context(), time.Now())

	assert.True(t, e.state.LastAttempt.IsZero())
	assert.False(t, e.dirty)
}

// TestFetchRecordsSuccess checks a good payload replaces the data and clears failures.
func TestFetchRecordsSuccess(t *testing.T) {
	t.Parallel()

	now := testNow
	body := testPayload(t, 30, 10, now.Add(time.Hour))

	fetcher := mocks.NewMockFetcher(t)
	fetcher.EXPECT().Fetch(mock.Anything, "secret-token").Return(body, nil).Once()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Debug(mock.Anything, "Fetched usage", mock.Anything).Once()

	e := newTestEngine(t, testOptions(), Deps{Fetcher: fetcher, Logger: logger})
	e.oauth, e.account = testLogin(now.Add(time.Hour)), "account-1"
	e.state.Backoff = schedule.Backoff{Until: now.Add(time.Minute), Kind: schedule.BackoffServer, Consecutive: 2}
	e.state.AuthBlock = &schedule.AuthBlock{Since: now.Add(-time.Hour), Reason: reasonUnauthorized}

	e.fetch(t.Context(), now)

	require.NotNil(t, e.state.Data)
	assert.Equal(t, now, e.state.Data.FetchedAt)
	assert.Equal(t, sourceAPI, e.state.Data.Source)
	assert.Equal(t, "account-1", e.state.Data.AccountUUID)
	assert.JSONEq(t, string(body), string(e.state.Data.Payload))
	assert.Equal(t, now, e.state.LastAttempt)
	assert.Equal(t, schedule.Backoff{}, e.state.Backoff)
	assert.Nil(t, e.state.AuthBlock)
	assert.True(t, e.dirty)
	assert.Len(t, e.usage.Bars, 2)
}

// TestFetchClassifiesFailures checks each failure becomes the right backoff or block.
//
// Login rejections become an auth block on the token rather than a backoff,
// so a new token from Claude Code is tried at once.
func TestFetchClassifiesFailures(t *testing.T) {
	t.Parallel()

	now := testNow
	expires := now.Add(time.Hour)

	tests := []struct {
		err         error
		wantBlock   *schedule.AuthBlock
		name        string
		wantKind    schedule.BackoffKind
		wantBackoff time.Duration
	}{
		{name: "plain error is a network failure", err: errBoom, wantKind: schedule.BackoffNetwork, wantBackoff: time.Minute},
		{
			name: "network", err: &usage.FetchError{Kind: usage.KindNetwork, Err: errBoom},
			wantKind: schedule.BackoffNetwork, wantBackoff: time.Minute,
		},
		{
			name: "server", err: &usage.FetchError{Kind: usage.KindServer, Status: http.StatusBadGateway},
			wantKind: schedule.BackoffServer, wantBackoff: 2 * time.Minute,
		},
		{
			name: "rate limited", err: &usage.FetchError{Kind: usage.KindRateLimited, RetryAfter: 12 * time.Minute},
			wantKind: schedule.BackoffRateLimited, wantBackoff: 12 * time.Minute,
		},
		{
			name: "wrapped rate limit", err: fmt.Errorf("fetch: %w", &usage.FetchError{Kind: usage.KindRateLimited}),
			wantKind: schedule.BackoffRateLimited, wantBackoff: 5 * time.Minute,
		},
		{
			name: "unknown kind", err: &usage.FetchError{Kind: usage.ErrKind(99)},
			wantKind: schedule.BackoffNetwork, wantBackoff: time.Minute,
		},
		{
			name: "unauthorized", err: &usage.FetchError{Kind: usage.KindUnauthorized, Status: http.StatusUnauthorized},
			wantBlock: &schedule.AuthBlock{TokenExpiresAt: expires, Since: now, Reason: reasonUnauthorized},
		},
		{
			name: "missing scope", err: &usage.FetchError{Kind: usage.KindScope, Status: http.StatusForbidden},
			wantBlock: &schedule.AuthBlock{TokenExpiresAt: expires, Since: now, Reason: reasonScope},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fetcher := mocks.NewMockFetcher(t)
			fetcher.EXPECT().Fetch(mock.Anything, "secret-token").Return(nil, tt.err).Once()

			logger := mocks.NewMockLogger(t)
			logger.EXPECT().Warn(mock.Anything, mock.Anything, mock.Anything).Once()

			e := newTestEngine(t, testOptions(), Deps{Fetcher: fetcher, Logger: logger})
			e.oauth = testLogin(expires)

			e.fetch(t.Context(), now)

			assert.Equal(t, now, e.state.LastAttempt)
			assert.Equal(t, tt.wantBlock, e.state.AuthBlock)

			if tt.wantBlock != nil {
				assert.Equal(t, schedule.Backoff{}, e.state.Backoff)

				return
			}

			assert.Equal(t, tt.wantKind, e.state.Backoff.Kind)
			assert.Equal(t, now.Add(tt.wantBackoff), e.state.Backoff.Until)
		})
	}
}

// TestFetchBacksOffOnAnUnrecognizedPayload checks a format change keeps the old data.
func TestFetchBacksOffOnAnUnrecognizedPayload(t *testing.T) {
	t.Parallel()

	now := testNow

	fetcher := mocks.NewMockFetcher(t)
	fetcher.EXPECT().Fetch(mock.Anything, mock.Anything).Return([]byte(`{"renamed":{}}`), nil).Once()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Warn(mock.Anything, "Unrecognized usage payload", mock.Anything).Once()

	e := newTestEngine(t, testOptions(), Deps{Fetcher: fetcher, Logger: logger})
	withData(t, e, testPayload(t, 10, 5, now.Add(time.Hour)), now.Add(-time.Hour))

	e.fetch(t.Context(), now)

	assert.Equal(t, schedule.BackoffBadPayload, e.state.Backoff.Kind)
	assert.Equal(t, now.Add(2*time.Minute), e.state.Backoff.Until)
	require.NotNil(t, e.state.Data)
	assert.Equal(t, now.Add(-time.Hour), e.state.Data.FetchedAt)
}

// TestFetchIgnoresACanceledContext checks shutdown is not recorded as a failure.
//
// A fetch cut short by shutdown says nothing about the endpoint, so it must
// not start a backoff that the next run would honor.
func TestFetchIgnoresACanceledContext(t *testing.T) {
	t.Parallel()

	now := testNow
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fetcher := mocks.NewMockFetcher(t)
	fetcher.EXPECT().Fetch(mock.Anything, mock.Anything).Return(nil, context.Canceled).Once()

	e := newTestEngine(t, testOptions(), Deps{Fetcher: fetcher, Logger: mocks.NewMockLogger(t)})

	e.fetch(ctx, now)

	assert.Equal(t, schedule.Backoff{}, e.state.Backoff)
	assert.Equal(t, now, e.state.LastAttempt)
}

// TestAdoptCache checks when Claude Code's cached usage replaces the data.
//
// Only a cache for the signed-in account that is newer than the data, not
// dated implausibly far in the future, and parseable is adopted.
func TestAdoptCache(t *testing.T) {
	t.Parallel()

	now := testNow
	resets := now.Add(time.Hour)

	cache := func(fetched time.Time, account string, payload []byte) claudecode.GlobalState {
		return claudecode.GlobalState{
			AccountUUID: "account-1",
			Cached:      &claudecode.CachedUsage{FetchedAt: fetched, AccountUUID: account, Payload: payload},
		}
	}

	tests := []struct {
		global    claudecode.GlobalState
		name      string
		wantAdopt bool
	}{
		{name: "no cache", global: claudecode.GlobalState{AccountUUID: "account-1"}},
		{name: "another account", global: cache(now, "account-2", testPayload(t, 50, 5, resets))},
		{name: "older than the data", global: cache(now.Add(-20*time.Minute), "account-1", testPayload(t, 50, 5, resets))},
		{name: "same age as the data", global: cache(now.Add(-10*time.Minute), "account-1", testPayload(t, 50, 5, resets))},
		{name: "dated far in the future", global: cache(now.Add(2*time.Minute), "account-1", testPayload(t, 50, 5, resets))},
		{name: "unparseable", global: cache(now, "account-1", []byte(`{"other":1}`))},
		{name: "fresher", global: cache(now.Add(-time.Minute), "account-1", testPayload(t, 50, 5, resets)), wantAdopt: true},
		{
			name: "slightly in the future", global: cache(now.Add(30*time.Second), "account-1", testPayload(t, 50, 5, resets)),
			wantAdopt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := newTestEngine(t, testOptions(), Deps{})
			withData(t, e, testPayload(t, 10, 5, resets), now.Add(-10*time.Minute))

			e.adoptCache(t.Context(), tt.global, now)

			if !tt.wantAdopt {
				assert.Equal(t, sourceAPI, e.state.Data.Source)
				assert.InDelta(t, 10.0, e.usage.Bars[0].Percent, 0.001)

				return
			}

			assert.Equal(t, sourceClaudeCode, e.state.Data.Source)
			assert.Equal(t, tt.global.Cached.FetchedAt, e.state.Data.FetchedAt)
			assert.InDelta(t, 50.0, e.usage.Bars[0].Percent, 0.001)
		})
	}
}

// TestObserveDropsDataOnAccountChange checks one account's usage is never shown for another.
func TestObserveDropsDataOnAccountChange(t *testing.T) {
	t.Parallel()

	now := testNow

	global := mocks.NewMockGlobalSource(t)
	global.EXPECT().Read().Return(claudecode.GlobalState{AccountUUID: "account-2"}, nil)

	logger := mocks.NewMockLogger(t)

	e := newTestEngine(t, testOptions(), Deps{})
	withData(t, e, testPayload(t, 10, 5, now.Add(time.Hour)), now.Add(-time.Minute))
	e.deps.Global, e.log, e.dirty = global, logger, false

	logger.EXPECT().Info(mock.Anything, "Signed-in account changed, dropping old data").Once()

	e.observe(t.Context(), now)

	assert.Nil(t, e.state.Data)
	assert.Empty(t, e.usage.Bars)
	assert.Equal(t, "account-2", e.account)
	assert.True(t, e.dirty)
}

// TestObserveKeepsDataWithoutAnAccountChange checks unknown accounts are not a change.
func TestObserveKeepsDataWithoutAnAccountChange(t *testing.T) {
	t.Parallel()

	now := testNow

	for _, account := range []string{"account-1", ""} {
		global := mocks.NewMockGlobalSource(t)
		global.EXPECT().Read().Return(claudecode.GlobalState{AccountUUID: account}, nil).Once()

		e := newTestEngine(t, testOptions(), Deps{})
		withData(t, e, testPayload(t, 10, 5, now.Add(time.Hour)), now.Add(-time.Minute))
		e.deps.Global = global

		e.observe(t.Context(), now)

		assert.NotNil(t, e.state.Data, "account %q", account)
	}
}

// TestObserveLogsAGlobalReadError checks a broken global config is only logged.
func TestObserveLogsAGlobalReadError(t *testing.T) {
	t.Parallel()

	global := mocks.NewMockGlobalSource(t)
	global.EXPECT().Read().Return(claudecode.GlobalState{AccountUUID: "account-1"}, errBoom).Twice()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Debug(mock.Anything, "Reading Claude Code's global config failed", mock.Anything).Twice()

	e := New(t.Context(), testOptions(), Deps{Store: new(state.Memory), Global: global, Logger: logger})
	e.observe(t.Context(), time.Now())

	assert.Equal(t, "account-1", e.account)
}

// TestObserveReadsActivityAndCredentials checks the inputs Decide depends on.
func TestObserveReadsActivityAndCredentials(t *testing.T) {
	t.Parallel()

	now := testNow
	login := testLogin(now.Add(time.Hour))

	activity := mocks.NewMockActivitySource(t)
	activity.EXPECT().Active(now).Return(true).Once()

	credentials := mocks.NewMockCredentialsSource(t)
	credentials.EXPECT().Read().Return(login, errBoom).Once()

	e := newTestEngine(t, testOptions(), Deps{})
	e.deps.Activity, e.deps.Credentials = activity, credentials

	e.observe(t.Context(), now)

	assert.True(t, e.active)
	assert.Equal(t, login, e.oauth)
	assert.ErrorIs(t, e.credErr, errBoom)
}

// TestObserveSkipsCredentialsInCacheOnlyMode checks cache-only never touches the login.
func TestObserveSkipsCredentialsInCacheOnlyMode(t *testing.T) {
	t.Parallel()

	opt := testOptions()
	opt.Mode = config.ModeCacheOnly

	e := newTestEngine(t, opt, Deps{Credentials: mocks.NewMockCredentialsSource(t)})

	e.observe(t.Context(), time.Now())

	assert.Equal(t, claudecode.OAuth{}, e.oauth)
}

// TestAlertWithoutNotifierDoesNothing checks a nil notifier disables alerts.
func TestAlertWithoutNotifierDoesNothing(t *testing.T) {
	t.Parallel()

	now := testNow
	e := newTestEngine(t, testOptions(), Deps{})
	withData(t, e, testPayload(t, 99, 99, now.Add(time.Hour)), now)
	e.dirty = false

	e.alert(t.Context(), now)

	assert.Empty(t, e.state.Alerts.Entries)
	assert.False(t, e.dirty)
}

// TestAlertRecordsSentThresholds checks a sent alert is remembered with its ID.
func TestAlertRecordsSentThresholds(t *testing.T) {
	t.Parallel()

	now := testNow

	notifier := mocks.NewMockNotifier(t)
	notifier.EXPECT().Send(mock.Anything, mock.MatchedBy(func(message notify.Message) bool {
		return message.Summary == "Current session at 85%" && message.ReplacesID == 0
	})).Return(uint32(4), nil).Once()

	e := newTestEngine(t, testOptions(), Deps{Notifier: notifier})
	withData(t, e, testPayload(t, 85, 10, now.Add(time.Hour)), now)
	e.dirty = false

	e.alert(t.Context(), now)
	e.alert(t.Context(), now.Add(time.Minute))

	assert.Equal(t, uint32(4), e.state.Alerts.ReplaceIDs["session"])
	require.Len(t, e.state.Alerts.Entries, 1)
	assert.InDelta(t, 80.0, e.state.Alerts.Entries[0].Threshold, 0.001)
	assert.True(t, e.dirty)
}

// TestAlertRetriesAfterASendFailure checks a failed alert is not recorded as sent.
func TestAlertRetriesAfterASendFailure(t *testing.T) {
	t.Parallel()

	now := testNow

	notifier := mocks.NewMockNotifier(t)
	notifier.EXPECT().Send(mock.Anything, mock.Anything).Return(uint32(0), errBoom).Once()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Warn(mock.Anything, "Notification failed", mock.Anything).Once()

	e := newTestEngine(t, testOptions(), Deps{Notifier: notifier, Logger: logger})
	withData(t, e, testPayload(t, 85, 10, now.Add(time.Hour)), now)
	e.dirty = false

	e.alert(t.Context(), now)

	assert.Empty(t, e.state.Alerts.Entries)
	assert.False(t, e.dirty)
}

// TestAuthAlertWaitsThenSendsOncePerToken checks the login alert's timing and key.
//
// The alert key includes the token's expiry, so a new token that is rejected
// again is a new problem worth one more alert.
func TestAuthAlertWaitsThenSendsOncePerToken(t *testing.T) {
	t.Parallel()

	now := testNow
	login := testLogin(now.Add(time.Hour))

	notifier := mocks.NewMockNotifier(t)
	notifier.EXPECT().Send(mock.Anything, notify.AuthMessage()).Return(uint32(1), nil).Twice()

	e := newTestEngine(t, testOptions(), Deps{Notifier: notifier})
	e.oauth = login
	e.decision.Gate = schedule.GateAuthBlocked

	e.authAlert(t.Context(), now)
	e.authAlert(t.Context(), now.Add(9*time.Minute))
	assert.Empty(t, e.state.Alerts.Auth)

	e.authAlert(t.Context(), now.Add(10*time.Minute))
	assert.Equal(t, "auth_error|"+strconv.FormatInt(login.ExpiresAt.UnixMilli(), 10), e.state.Alerts.Auth)

	e.authAlert(t.Context(), now.Add(30*time.Minute))
	notifier.AssertNumberOfCalls(t, "Send", 1)

	e.oauth.ExpiresAt = now.Add(2 * time.Hour)
	e.authAlert(t.Context(), now.Add(31*time.Minute))
	notifier.AssertNumberOfCalls(t, "Send", 2)
}

// TestAuthAlertClearsWhenTheLoginRecovers checks recovery rearms the alert.
func TestAuthAlertClearsWhenTheLoginRecovers(t *testing.T) {
	t.Parallel()

	now := testNow
	e := newTestEngine(t, testOptions(), Deps{Notifier: mocks.NewMockNotifier(t)})
	e.state.Alerts.Auth = "logged_out|0"
	e.authBadSince = now.Add(-time.Hour)
	e.decision.Gate = schedule.GateNone

	e.authAlert(t.Context(), now)

	assert.Empty(t, e.state.Alerts.Auth)
	assert.True(t, e.authBadSince.IsZero())
	assert.True(t, e.dirty)
}

// TestAuthAlertHonorsTheSetting checks NotifyAuth false silences the login alert.
func TestAuthAlertHonorsTheSetting(t *testing.T) {
	t.Parallel()

	opt := testOptions()
	opt.NotifyAuth = false

	now := testNow
	e := newTestEngine(t, opt, Deps{Notifier: mocks.NewMockNotifier(t)})
	e.decision.Gate = schedule.GateLoggedOut

	e.authAlert(t.Context(), now)
	e.authAlert(t.Context(), now.Add(time.Hour))

	assert.Empty(t, e.state.Alerts.Auth)
}

// TestAuthAlertRetriesAfterASendFailure checks a failed login alert is retried.
func TestAuthAlertRetriesAfterASendFailure(t *testing.T) {
	t.Parallel()

	now := testNow

	notifier := mocks.NewMockNotifier(t)
	notifier.EXPECT().Send(mock.Anything, mock.Anything).Return(uint32(0), errBoom).Once()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Warn(mock.Anything, "Notification failed", mock.Anything).Once()

	e := newTestEngine(t, testOptions(), Deps{Notifier: notifier, Logger: logger})
	e.decision.Gate = schedule.GateCredentials
	e.authBadSince = now.Add(-time.Hour)

	e.authAlert(t.Context(), now)

	assert.Empty(t, e.state.Alerts.Auth)
}

// TestNextResetPicksTheEarliestFutureReset checks only resets after the data count.
func TestNextResetPicksTheEarliestFutureReset(t *testing.T) {
	t.Parallel()

	now := testNow
	e := newTestEngine(t, testOptions(), Deps{})

	assert.True(t, e.nextReset().IsZero())

	withData(t, e, testPayload(t, 10, 5, now.Add(time.Hour)), now)
	e.usage.Bars = append(e.usage.Bars,
		usage.Bar{ID: "past", ResetsAt: now.Add(-time.Minute)},
		usage.Bar{ID: "none"},
		usage.Bar{ID: "later", ResetsAt: now.Add(3 * time.Hour)},
	)

	assert.Equal(t, now.Add(time.Hour), e.nextReset())
}

// TestStepFetchesAndSaves checks a due wake fetches, publishes, and persists.
func TestStepFetchesAndSaves(t *testing.T) {
	t.Parallel()

	now := testNow

	store := mocks.NewMockStore(t)
	store.EXPECT().Load().Return(state.Fresh(), nil).Once()
	store.EXPECT().Save(mock.MatchedBy(func(saved state.State) bool {
		return saved.Data != nil && saved.LastAttempt.Equal(now.Add(time.Minute))
	})).Return(nil).Once()

	credentials := mocks.NewMockCredentialsSource(t)
	credentials.EXPECT().Read().Return(testLogin(now.Add(time.Hour)), nil)

	fetcher := mocks.NewMockFetcher(t)
	fetcher.EXPECT().Fetch(mock.Anything, "secret-token").Return(testPayload(t, 10, 5, now.Add(time.Hour)), nil).Once()

	e := New(t.Context(), testOptions(), Deps{Store: store, Credentials: credentials, Fetcher: fetcher})
	e.startedAt = now

	var published []string

	e.SetPublisher(func(snap string) { published = append(published, snap) })

	e.step(t.Context(), now.Add(time.Minute), false)

	assert.False(t, e.dirty)
	require.Len(t, published, 1)
	assert.Contains(t, published[0], `"status":"ok"`)
	assert.Equal(t, now.Add(time.Minute+20*time.Minute), e.decision.NextFetchAt, "idle without an activity source")

	e.step(t.Context(), now.Add(2*time.Minute), false)
	assert.Len(t, published, 1, "a quiet wake neither publishes nor saves")
}

// TestStepKeepsStateDirtyWhenSavingFails checks a failed save is retried next wake.
func TestStepKeepsStateDirtyWhenSavingFails(t *testing.T) {
	t.Parallel()

	store := mocks.NewMockStore(t)
	store.EXPECT().Load().Return(state.Fresh(), nil).Once()
	store.EXPECT().Save(mock.Anything).Return(errBoom).Twice()

	logger := mocks.NewMockLogger(t)
	logger.EXPECT().Warn(mock.Anything, "Saving state failed", mock.Anything).Twice()

	e := New(t.Context(), testOptions(), Deps{Store: store, Logger: logger})
	e.dirty = true

	e.step(t.Context(), time.Now(), false)
	assert.True(t, e.dirty)

	e.step(t.Context(), time.Now(), false)
	assert.True(t, e.dirty)
}

// TestRunSavesOnShutdown checks a canceled loop persists the state.
func TestRunSavesOnShutdown(t *testing.T) {
	t.Parallel()

	store := mocks.NewMockStore(t)
	store.EXPECT().Load().Return(state.Fresh(), nil).Once()
	store.EXPECT().Save(mock.Anything).Return(nil).Once()

	e := New(t.Context(), testOptions(), Deps{Store: store})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	e.Run(ctx)
}
