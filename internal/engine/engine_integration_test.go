// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package engine_test runs the engine loop in a synctest bubble.
//
// Time inside a bubble is simulated and starts at 2000-01-01, so each test
// covers hours of polling in microseconds and checks exactly when the engine
// calls the endpoint and what it publishes.
package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/engine"
	"github.com/nicholas-fedor/clankerwatch/internal/engine/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// fakeFetcher answers fetches from a script and records when they happened.
type fakeFetcher struct {
	respond func(n int) ([]byte, error)
	calls   []time.Time
	mu      sync.Mutex
}

// fakeCredentials returns a login the test can change while the engine runs.
type fakeCredentials struct {
	err   error
	oauth claudecode.OAuth
	mu    sync.Mutex
}

// fakeGlobal returns a global state the test can change while the engine runs.
type fakeGlobal struct {
	state claudecode.GlobalState
	mu    sync.Mutex
}

// recorder keeps every published snapshot.
type recorder struct {
	snaps []string
	mu    sync.Mutex
}

// running is an engine whose Run loop is in progress.
type running struct {
	eng    *engine.Engine
	rec    *recorder
	cancel context.CancelFunc
	done   chan struct{}
}

// snapshotView is the part of the snapshot JSON the tests read.
type snapshotView struct {
	RefreshAllowedAtMs *int64     `json:"refreshAllowedAtMs"`
	ExtraUsage         *extraView `json:"extraUsage"`
	Status             string     `json:"status"`
	Message            string     `json:"message"`
	Mode               string     `json:"mode"`
	Source             string     `json:"source"`
	Plan               string     `json:"plan"`
	Bars               []barView  `json:"bars"`
	V                  int        `json:"v"`
	FetchedAtMs        int64      `json:"fetchedAtMs"`
	NextUpdateAtMs     int64      `json:"nextUpdateAtMs"`
	LoginExpiresAtMs   int64      `json:"loginExpiresAtMs"`
	WarnAt             float64    `json:"warnAt"`
	CritAt             float64    `json:"critAt"`
	Active             bool       `json:"active"`
}

// barView is one bar of the snapshot JSON.
type barView struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Percent  float64 `json:"percent"`
	Level    int     `json:"level"`
	Headline bool    `json:"headline"`
}

// extraView is the extra usage section of the snapshot JSON.
type extraView struct {
	Percent      *float64 `json:"percent"`
	Used         string   `json:"used"`
	Limit        string   `json:"limit"`
	Level        int      `json:"level"`
	LimitReached bool     `json:"limitReached"`
}

// accountUUID is the signed-in account in these tests.
const accountUUID = "account-1"

// Fetch records the call and returns the scripted response.
func (f *fakeFetcher) Fetch(context.Context, string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := len(f.calls)
	f.calls = append(f.calls, time.Now())

	return f.respond(n)
}

// count returns how many fetches happened.
func (f *fakeFetcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.calls)
}

// at returns when the nth fetch, counting from 0, happened.
func (f *fakeFetcher) at(n int) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls[n]
}

// Read returns the current login.
func (c *fakeCredentials) Read() (claudecode.OAuth, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.oauth, c.err
}

// set replaces the login.
func (c *fakeCredentials) set(oauth claudecode.OAuth, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.oauth, c.err = oauth, err
}

// Read returns the current global state.
func (g *fakeGlobal) Read() (claudecode.GlobalState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.state, nil
}

// set replaces the global state.
func (g *fakeGlobal) set(global claudecode.GlobalState) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.state = global
}

// publish records a snapshot.
func (r *recorder) publish(snap string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.snaps = append(r.snaps, snap)
}

// count returns how many snapshots were published.
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.snaps)
}

// first returns the first published snapshot.
func (r *recorder) first() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.snaps[0]
}

// login returns a usable Max 5x login that expires at expiresAt.
//
// Parameters:
//   - expiresAt: the token expiry.
//
// Returns:
//   - claudecode.OAuth: the login.
func login(expiresAt time.Time) claudecode.OAuth {
	return claudecode.OAuth{
		AccessToken:      "token",
		ExpiresAt:        expiresAt,
		LoginExpiresAt:   expiresAt.Add(30 * 24 * time.Hour),
		Scopes:           []string{"user:inference", "user:profile"},
		SubscriptionType: "max",
		RateLimitTier:    "default_claude_max_5x",
	}
}

// payload renders a usage payload with a session and a weekly bar.
//
// Parameters:
//   - t: test handle.
//   - session: the session percentage.
//   - weekly: the weekly percentage.
//   - resets: when the session resets. The week resets six days later.
//
// Returns:
//   - []byte: the payload JSON.
func payload(t *testing.T, session, weekly float64, resets time.Time) []byte {
	t.Helper()

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
			"currency": "EUR", "decimal_places": 2,
		},
	}

	content, err := json.Marshal(body)
	require.NoError(t, err)

	return content
}

// options returns hybrid-mode options with the production schedule.
//
// Returns:
//   - engine.Options: the options.
func options() engine.Options {
	return engine.Options{
		Mode:       config.ModeHybrid,
		Policy:     schedule.DefaultPolicy(5*time.Minute, 20*time.Minute),
		Thresholds: []float64{80, 95},
		Warn:       80,
		Crit:       95,
		NotifyAuth: true,
	}
}

// activeSource returns a mock that always reports Claude Code as active.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - *mocks.MockActivitySource: the mock.
func activeSource(t *testing.T) *mocks.MockActivitySource {
	t.Helper()

	activity := mocks.NewMockActivitySource(t)
	activity.EXPECT().Active(mock.Anything).Return(true)

	return activity
}

// start builds an engine and runs it in the background.
//
// Parameters:
//   - t: test handle.
//   - opt: the options.
//   - deps: the collaborators.
//
// Returns:
//   - *running: the running engine.
func start(t *testing.T, opt engine.Options, deps engine.Deps) *running {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	run := &running{
		eng:    engine.New(ctx, opt, deps),
		rec:    &recorder{},
		cancel: cancel,
		done:   make(chan struct{}),
	}

	run.eng.SetPublisher(run.rec.publish)

	go func() {
		defer close(run.done)

		run.eng.Run(ctx)
	}()

	synctest.Wait()

	return run
}

// stop cancels the loop and waits for it to save and return.
func (r *running) stop() {
	r.cancel()
	<-r.done
}

// view decodes the engine's current snapshot.
//
// Parameters:
//   - t: test handle.
//
// Returns:
//   - snapshotView: the decoded snapshot.
func (r *running) view(t *testing.T) snapshotView {
	t.Helper()

	return decode(t, r.eng.Snapshot())
}

// decode parses a snapshot.
//
// Parameters:
//   - t: test handle.
//   - snap: the snapshot JSON.
//
// Returns:
//   - snapshotView: the decoded snapshot.
func decode(t *testing.T, snap string) snapshotView {
	t.Helper()

	var view snapshotView

	require.NoError(t, json.Unmarshal([]byte(snap), &view))

	return view
}

// TestRunFetchesAfterTheStartupDelay checks the first fetch and publication.
//
// The engine waits ten seconds before its first request, publishes once when
// data arrives, and publishes nothing on the probe wakes in between because
// the snapshot bytes do not change.
func TestRunFetchesAfterTheStartupDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(int) ([]byte, error) {
			return payload(t, 20, 10, t0.Add(5*time.Hour)), nil
		}}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		assert.Equal(t, "starting", run.view(t).Status)

		synctest.Sleep(9 * time.Second)
		assert.Zero(t, fetcher.count())
		assert.Zero(t, run.rec.count(), "an unchanged snapshot is not published")

		synctest.Sleep(2 * time.Second)
		require.Equal(t, 1, fetcher.count())
		assert.Equal(t, t0.Add(10*time.Second), fetcher.at(0))
		assert.Equal(t, 1, run.rec.count())

		view := run.view(t)
		assert.Equal(t, "ok", view.Status)
		assert.Equal(t, "api", view.Source)
		assert.Equal(t, t0.Add(10*time.Second).UnixMilli(), view.FetchedAtMs)
		assert.Equal(t, t0.Add(10*time.Second+5*time.Minute).UnixMilli(), view.NextUpdateAtMs)

		synctest.Sleep(4 * time.Minute)
		assert.Equal(t, 1, fetcher.count())
		assert.Equal(t, 1, run.rec.count(), "probe wakes publish nothing")

		synctest.Sleep(time.Minute)
		assert.Equal(t, 2, fetcher.count())
		assert.Equal(t, t0.Add(10*time.Second+5*time.Minute), fetcher.at(1))
	})
}

// TestRunHonorsRetryAfter checks a 429 stops fetching until Retry-After passes.
//
// A widget refresh during the backoff is ignored, because a request then
// would only extend the rate limit.
func TestRunHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(n int) ([]byte, error) {
			if n == 0 {
				return nil, &usage.FetchError{
					Kind: usage.KindRateLimited, Status: http.StatusTooManyRequests, RetryAfter: 600 * time.Second,
				}
			}

			return payload(t, 20, 10, t0.Add(5*time.Hour)), nil
		}}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(11 * time.Second)
		require.Equal(t, 1, fetcher.count())

		until := t0.Add(10*time.Second + 10*time.Minute).UnixMilli()
		view := run.view(t)
		assert.Equal(t, "rate_limited", view.Status)
		assert.Equal(t, "The usage endpoint is rate limited", view.Message)
		assert.Equal(t, until, view.NextUpdateAtMs)
		require.NotNil(t, view.RefreshAllowedAtMs)
		assert.Equal(t, until, *view.RefreshAllowedAtMs)

		published := run.rec.count()

		synctest.Sleep(time.Minute)
		run.eng.RequestRefresh()
		synctest.Wait()

		assert.Equal(t, 1, fetcher.count(), "a refresh during the backoff is ignored")

		synctest.Sleep(8*time.Minute + 50*time.Second)
		assert.Equal(t, 1, fetcher.count())
		assert.Equal(t, published, run.rec.count())

		synctest.Sleep(10 * time.Second)
		require.Equal(t, 2, fetcher.count())
		assert.Equal(t, t0.Add(10*time.Second+10*time.Minute), fetcher.at(1))
		assert.Equal(t, "ok", run.view(t).Status)
	})
}

// TestRunAdoptsFresherClaudeCodeCache checks Claude Code's own fetch counts.
//
// Claude Code caches the usage it fetches. Adopting a fresher cache shows it
// at once and pushes the next request back, since the endpoint was just
// called.
func TestRunAdoptsFresherClaudeCodeCache(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		resets := t0.Add(5 * time.Hour)
		fetcher := &fakeFetcher{respond: func(int) ([]byte, error) { return payload(t, 20, 10, resets), nil }}
		global := &fakeGlobal{state: claudecode.GlobalState{AccountUUID: accountUUID}}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Global:      global,
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(11 * time.Second)
		require.Equal(t, 1, fetcher.count())

		global.set(claudecode.GlobalState{
			AccountUUID: accountUUID,
			Cached: &claudecode.CachedUsage{
				FetchedAt:   t0.Add(time.Minute),
				AccountUUID: accountUUID,
				Payload:     payload(t, 30, 10, resets),
			},
		})

		synctest.Sleep(2 * time.Minute)

		view := run.view(t)
		assert.Equal(t, "claude-code", view.Source)
		assert.Equal(t, t0.Add(time.Minute).UnixMilli(), view.FetchedAtMs)
		require.NotEmpty(t, view.Bars)
		assert.InDelta(t, 30.0, view.Bars[0].Percent, 0.001)
		assert.Equal(t, t0.Add(6*time.Minute).UnixMilli(), view.NextUpdateAtMs)

		synctest.Sleep(3 * time.Minute)
		assert.Equal(t, 1, fetcher.count(), "the adopted cache suppresses the fetch due at 5m10s")

		synctest.Sleep(time.Minute)
		require.Equal(t, 2, fetcher.count())
		assert.Equal(t, t0.Add(6*time.Minute), fetcher.at(1))
		assert.Equal(t, "api", run.view(t).Source)
	})
}

// TestRunWaitsForAnExpiringTokenToBeRefreshed checks the engine never uses a dying token.
//
// The engine cannot refresh the token itself, so it reports token_expired
// and fetches only once Claude Code writes a new one. An expiring token is not
// a login problem, so no alert is sent.
func TestRunWaitsForAnExpiringTokenToBeRefreshed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(int) ([]byte, error) { return payload(t, 20, 10, t0.Add(5*time.Hour)), nil }}
		credentials := &fakeCredentials{oauth: login(t0.Add(3 * time.Minute))}

		run := start(t, options(), engine.Deps{
			Credentials: credentials,
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Notifier:    mocks.NewMockNotifier(t),
			Store:       new(state.Memory),
		})
		defer run.stop()

		view := run.view(t)
		assert.Equal(t, "token_expired", view.Status)
		assert.Equal(t, "Waiting for Claude Code to refresh its login", view.Message)

		synctest.Sleep(5 * time.Minute)
		assert.Zero(t, fetcher.count())
		assert.Equal(t, "token_expired", run.view(t).Status)

		credentials.set(login(t0.Add(8*time.Hour)), nil)

		synctest.Sleep(time.Minute + time.Second)
		require.Equal(t, 1, fetcher.count())
		assert.Equal(t, t0.Add(6*time.Minute), fetcher.at(0))
		assert.Equal(t, "ok", run.view(t).Status)
	})
}

// TestRunRestartKeepsBackoffAndAlerts checks state survives a restart.
//
// The first engine alerts at 80% and is then rate limited. A second engine on
// the same store must keep waiting out the Retry-After, must not repeat the
// 80% alert, and must replace that notification when 95% is crossed.
func TestRunRestartKeepsBackoffAndAlerts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		resets := t0.Add(5 * time.Hour)
		fetcher := &fakeFetcher{respond: func(n int) ([]byte, error) {
			switch n {
			case 0:
				return payload(t, 85, 10, resets), nil
			case 1:
				return nil, &usage.FetchError{
					Kind: usage.KindRateLimited, Status: http.StatusTooManyRequests, RetryAfter: 600 * time.Second,
				}
			default:
				return payload(t, 96, 10, resets), nil
			}
		}}
		store := new(state.Memory)
		notifier := mocks.NewMockNotifier(t)

		notifier.EXPECT().Send(mock.Anything, mock.MatchedBy(func(message notify.Message) bool {
			return message.Summary == "Current session at 85%" && message.ReplacesID == 0
		})).Return(7, nil).Once()
		notifier.EXPECT().Send(mock.Anything, mock.MatchedBy(func(message notify.Message) bool {
			return message.Summary == "Current session at 96%" && message.ReplacesID == 7
		})).Return(8, nil).Once()

		deps := engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Notifier:    notifier,
			Store:       store,
		}

		first := start(t, options(), deps)

		synctest.Sleep(6 * time.Minute)
		require.Equal(t, 2, fetcher.count())
		assert.Equal(t, "rate_limited", first.view(t).Status)
		first.stop()

		second := start(t, options(), deps)
		defer second.stop()

		view := second.view(t)
		assert.Equal(t, "rate_limited", view.Status)
		require.NotEmpty(t, view.Bars)
		assert.InDelta(t, 85.0, view.Bars[0].Percent, 0.001)

		synctest.Sleep(9 * time.Minute)
		assert.Equal(t, 2, fetcher.count(), "the restored backoff still holds")

		synctest.Sleep(11 * time.Second)
		require.Equal(t, 3, fetcher.count())
		assert.Equal(t, t0.Add(5*time.Minute+10*time.Second+10*time.Minute), fetcher.at(2))
		assert.Equal(t, "ok", second.view(t).Status)

		saved, err := store.Load()
		require.NoError(t, err)
		assert.Equal(t, uint32(8), saved.Alerts.ReplaceIDs["session"])
	})
}

// TestRunAlertsALoginProblemOnce checks the login alert waits and does not repeat.
//
// A brief logout during a Claude Code restart is not worth a notification,
// so the alert waits ten minutes, and it is sent only once per problem.
func TestRunAlertsALoginProblemOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		credentials := mocks.NewMockCredentialsSource(t)
		credentials.EXPECT().Read().Return(claudecode.OAuth{}, claudecode.ErrLoggedOut)

		notifier := mocks.NewMockNotifier(t)
		notifier.EXPECT().Send(mock.Anything, notify.AuthMessage()).Return(1, nil).Once()

		run := start(t, options(), engine.Deps{
			Credentials: credentials,
			Activity:    activeSource(t),
			Fetcher:     mocks.NewMockFetcher(t),
			Notifier:    notifier,
			Store:       new(state.Memory),
		})
		defer run.stop()

		view := run.view(t)
		assert.Equal(t, "logged_out", view.Status)
		assert.Equal(t, "Claude Code is not signed in", view.Message)
		assert.Nil(t, view.RefreshAllowedAtMs)

		synctest.Sleep(9 * time.Minute)
		notifier.AssertNotCalled(t, "Send", mock.Anything, mock.Anything)

		synctest.Sleep(time.Hour)
		notifier.AssertNumberOfCalls(t, "Send", 1)
	})
}

// TestRunCacheOnlyNeverFetches checks cache-only mode stays off the network.
//
// It reads neither the credentials nor the endpoint, reports no_data until
// Claude Code caches usage, and always allows a refresh.
func TestRunCacheOnlyNeverFetches(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		global := &fakeGlobal{state: claudecode.GlobalState{AccountUUID: accountUUID}}
		opt := options()
		opt.Mode = config.ModeCacheOnly

		run := start(t, opt, engine.Deps{
			Credentials: mocks.NewMockCredentialsSource(t),
			Global:      global,
			Activity:    activeSource(t),
			Fetcher:     mocks.NewMockFetcher(t),
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(30 * time.Minute)
		run.eng.RequestRefresh()
		synctest.Wait()

		view := run.view(t)
		assert.Equal(t, "no_data", view.Status)
		assert.Equal(t, "cache-only", view.Mode)
		assert.Zero(t, view.NextUpdateAtMs)
		require.NotNil(t, view.RefreshAllowedAtMs)
		assert.Zero(t, *view.RefreshAllowedAtMs)
		assert.Empty(t, view.Plan, "cache-only never reads the login")

		global.set(claudecode.GlobalState{
			AccountUUID: accountUUID,
			Cached: &claudecode.CachedUsage{
				FetchedAt:   t0.Add(29 * time.Minute),
				AccountUUID: accountUUID,
				Payload:     payload(t, 40, 10, t0.Add(5*time.Hour)),
			},
		})

		synctest.Sleep(2 * time.Minute)

		view = run.view(t)
		assert.Equal(t, "ok", view.Status)
		assert.Equal(t, "claude-code", view.Source)
	})
}

// TestRunDropsDataOfAnotherAccount checks a new sign-in hides the old account's usage.
func TestRunDropsDataOfAnotherAccount(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		store := new(state.Memory)
		saved := state.Fresh()
		saved.Data = &state.Data{
			FetchedAt:   t0.Add(-time.Minute),
			Source:      "api",
			AccountUUID: "old-account",
			Payload:     payload(t, 70, 50, t0.Add(5*time.Hour)),
		}
		require.NoError(t, store.Save(saved))

		fetcher := &fakeFetcher{respond: func(int) ([]byte, error) { return payload(t, 5, 1, t0.Add(5*time.Hour)), nil }}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Global:      &fakeGlobal{state: claudecode.GlobalState{AccountUUID: accountUUID}},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       store,
		})

		view := run.view(t)
		assert.Equal(t, "starting", view.Status)
		assert.Empty(t, view.Bars)

		synctest.Sleep(11 * time.Second)
		require.Equal(t, 1, fetcher.count())
		run.stop()

		loaded, err := store.Load()
		require.NoError(t, err)
		require.NotNil(t, loaded.Data)
		assert.Equal(t, accountUUID, loaded.Data.AccountUUID)
	})
}

// TestRunBacksOffOnAnUnrecognizedPayload checks a format change is reported, not hammered.
func TestRunBacksOffOnAnUnrecognizedPayload(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(n int) ([]byte, error) {
			if n == 0 {
				return []byte(`{"something_new":{"value":1}}`), nil
			}

			return payload(t, 20, 10, t0.Add(5*time.Hour)), nil
		}}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(11 * time.Second)
		require.Equal(t, 1, fetcher.count())

		view := run.view(t)
		assert.Equal(t, "error", view.Status)
		assert.Equal(t, "The usage endpoint returned an unrecognized payload", view.Message)
		assert.Equal(t, t0.Add(10*time.Second+2*time.Minute).UnixMilli(), view.NextUpdateAtMs)

		// The error status holds until the retry, which happens when the
		// backoff ends at 2m10s, as the snapshot promised.
		synctest.Sleep(2*time.Minute - 2*time.Second)
		assert.Equal(t, 1, fetcher.count())
		assert.Equal(t, "error", run.view(t).Status)

		synctest.Sleep(time.Second)
		require.Equal(t, 2, fetcher.count())
		assert.Equal(t, t0.Add(2*time.Minute+10*time.Second), fetcher.at(1))
		assert.Equal(t, "ok", run.view(t).Status)
	})
}

// TestRunBlocksARejectedToken checks a 401 stops requests with that token.
//
// The same token is retried only after an hour, and a new token from Claude
// Code lifts the block at once.
func TestRunBlocksARejectedToken(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(n int) ([]byte, error) {
			if n == 0 {
				return nil, &usage.FetchError{Kind: usage.KindUnauthorized, Status: http.StatusUnauthorized}
			}

			return payload(t, 20, 10, t0.Add(5*time.Hour)), nil
		}}
		credentials := &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))}

		run := start(t, options(), engine.Deps{
			Credentials: credentials,
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(11 * time.Second)
		require.Equal(t, 1, fetcher.count())

		view := run.view(t)
		assert.Equal(t, "auth_error", view.Status)
		assert.Equal(t, "The usage endpoint rejected Claude Code's login", view.Message)
		assert.Nil(t, view.RefreshAllowedAtMs)

		run.eng.RequestRefresh()
		synctest.Sleep(19 * time.Minute)
		assert.Equal(t, 1, fetcher.count())

		credentials.set(login(t0.Add(9*time.Hour)), nil)

		synctest.Sleep(2 * time.Minute)
		require.Equal(t, 2, fetcher.count())
		assert.Equal(t, "ok", run.view(t).Status)
	})
}

// TestRunRetriesARejectedTokenAfterAnHour checks the auth block expires.
func TestRunRetriesARejectedTokenAfterAnHour(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(int) ([]byte, error) {
			return nil, &usage.FetchError{Kind: usage.KindScope, Status: http.StatusForbidden}
		}}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(59 * time.Minute)
		assert.Equal(t, 1, fetcher.count())
		assert.Equal(t, "auth_error", run.view(t).Status)

		synctest.Sleep(2 * time.Minute)
		require.Equal(t, 2, fetcher.count())
		assert.Equal(t, t0.Add(time.Hour+10*time.Second), fetcher.at(1))
	})
}

// TestRunReportsAnUnreachableEndpoint checks a transport error is shown as offline.
func TestRunReportsAnUnreachableEndpoint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(int) ([]byte, error) {
			return nil, errors.New("dial tcp: no route to host")
		}}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(11 * time.Second)

		view := run.view(t)
		assert.Equal(t, "offline", view.Status)
		assert.Equal(t, t0.Add(10*time.Second+time.Minute).UnixMilli(), view.NextUpdateAtMs)
	})
}

// TestRunPublishesTheSnapshotShape checks the JSON the widget parses.
func TestRunPublishesTheSnapshotShape(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		fetcher := &fakeFetcher{respond: func(int) ([]byte, error) { return payload(t, 85, 96, t0.Add(5*time.Hour)), nil }}

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     fetcher,
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(11 * time.Second)
		require.Equal(t, 1, run.rec.count())

		published := run.rec.first()

		assert.Equal(t, run.eng.Snapshot(), published)

		var raw map[string]json.RawMessage

		require.NoError(t, json.Unmarshal([]byte(published), &raw))

		for _, key := range []string{
			"v", "status", "message", "mode", "source", "active", "plan", "fetchedAtMs", "nextUpdateAtMs",
			"refreshAllowedAtMs", "loginExpiresAtMs", "warnAt", "critAt", "bars", "extraUsage",
		} {
			assert.Contains(t, raw, key)
		}

		view := decode(t, published)
		assert.Equal(t, engine.SnapshotVersion, view.V)
		assert.Equal(t, "ok", view.Status)
		assert.Empty(t, view.Message)
		assert.Equal(t, "hybrid", view.Mode)
		assert.True(t, view.Active)
		assert.Equal(t, "Max 5x", view.Plan)
		assert.Equal(t, t0.Add(8*time.Hour+30*24*time.Hour).UnixMilli(), view.LoginExpiresAtMs)
		assert.InDelta(t, 80.0, view.WarnAt, 0.001)
		assert.InDelta(t, 95.0, view.CritAt, 0.001)
		require.NotNil(t, view.RefreshAllowedAtMs)
		assert.Equal(t, t0.Add(10*time.Second+2*time.Minute).UnixMilli(), *view.RefreshAllowedAtMs)

		require.Len(t, view.Bars, 2)
		assert.Equal(t, barView{ID: "session", Label: "Current session", Percent: 85, Level: 1}, view.Bars[0])
		assert.Equal(t, barView{ID: "weekly_all", Label: "Current week (all models)", Percent: 96, Level: 2, Headline: true}, view.Bars[1])

		require.NotNil(t, view.ExtraUsage)
		assert.Equal(t, "€12.50", view.ExtraUsage.Used)
		assert.Equal(t, "€50.00", view.ExtraUsage.Limit)
		require.NotNil(t, view.ExtraUsage.Percent)
		assert.InDelta(t, 25.0, *view.ExtraUsage.Percent, 0.001)
		assert.Zero(t, view.ExtraUsage.Level)
		assert.False(t, view.ExtraUsage.LimitReached)
		assert.NotContains(t, published, "token", "the token never reaches the snapshot")
	})
}

// usageServer is an in-memory usage endpoint that records each request.
type usageServer struct {
	body    []byte
	headers []http.Header
	times   []time.Time
	mu      sync.Mutex
}

// ServeHTTP answers the first request with a 429 and a ten-minute
// Retry-After, and later requests with the server's payload.
//
// Parameters:
//   - w: the response writer.
//   - r: the request.
func (s *usageServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.headers = append(s.headers, r.Header.Clone())
	s.times = append(s.times, time.Now())
	first := len(s.headers) == 1
	s.mu.Unlock()

	if r.URL.Path != "/api/oauth/usage" {
		http.NotFound(w, r)

		return
	}

	if first {
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.body)
}

// requests returns the recorded request headers and times.
//
// Returns:
//   - []http.Header: the headers of each request.
//   - []time.Time: when each request arrived.
func (s *usageServer) requests() ([]http.Header, []time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]http.Header(nil), s.headers...), append([]time.Time(nil), s.times...)
}

// TestRunAgainstTheUsageClient drives the engine through the real usage
// client over an in-memory network.
//
// httptest.NewTestServer keeps the HTTP exchange inside the synctest bubble,
// so the 429, its Retry-After, and the retry all run in simulated time. The
// test checks what the endpoint receives as well as what the widget sees.
func TestRunAgainstTheUsageClient(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		endpoint := &usageServer{body: payload(t, 42, 17, t0.Add(4*time.Hour)), headers: nil, times: nil, mu: sync.Mutex{}}
		server := httptest.NewTestServer(t, endpoint)

		run := start(t, options(), engine.Deps{
			Credentials: &fakeCredentials{oauth: login(t0.Add(8 * time.Hour))},
			Activity:    activeSource(t),
			Fetcher:     usage.NewClientWithBase(server.URL, server.Client(), "test"),
			Store:       new(state.Memory),
		})
		defer run.stop()

		synctest.Sleep(11 * time.Second)

		view := run.view(t)
		assert.Equal(t, "rate_limited", view.Status)
		assert.Equal(t, t0.Add(10*time.Second+10*time.Minute).UnixMilli(), view.NextUpdateAtMs)

		synctest.Sleep(10 * time.Minute)

		headers, times := endpoint.requests()
		require.Len(t, headers, 2, "one attempt and one retry after Retry-After")
		assert.Equal(t, t0.Add(10*time.Second), times[0])
		assert.Equal(t, t0.Add(10*time.Second+10*time.Minute), times[1])

		for _, header := range headers {
			assert.Equal(t, "Bearer token", header.Get("Authorization"))
			assert.Equal(t, "oauth-2025-04-20", header.Get("Anthropic-Beta"))
			assert.Equal(t, "clankerwatch/test", header.Get("User-Agent"))
		}

		view = run.view(t)
		assert.Equal(t, "ok", view.Status)
		assert.Equal(t, "api", view.Source)
		require.Len(t, view.Bars, 2)
		assert.InDelta(t, 42.0, view.Bars[0].Percent, 0.001)
		assert.InDelta(t, 17.0, view.Bars[1].Percent, 0.001)
	})
}
