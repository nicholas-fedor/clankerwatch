// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/notify"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// Engine owns the usage data and failure state.
//
// Run drives it from a single goroutine. Snapshot, SetPublisher, and
// RequestRefresh are safe to call from any goroutine.
type Engine struct {
	deps         Deps
	authBadSince time.Time
	startedAt    time.Time
	log          Logger
	credErr      error
	refresh      chan struct{}
	publish      func(string)
	decision     schedule.Decision
	snapshot     string
	account      string
	oauth        claudecode.OAuth
	usage        usage.Usage
	state        state.State
	opt          Options
	mu           sync.Mutex
	active       bool
	dirty        bool
}

const (
	// fetchTimeout bounds one fetch, including the client's own timeouts.
	fetchTimeout = 30 * time.Second

	// authAlertAfter is how long a login problem lasts before it is alerted.
	authAlertAfter = 10 * time.Minute

	// clockSkew is how far in the future Claude Code's cache may be dated.
	clockSkew = time.Minute

	// minWait is the shortest sleep between steps.
	minWait = time.Second

	// reasonUnauthorized marks a token the server rejected.
	reasonUnauthorized = "unauthorized"

	// reasonScope marks a token without the profile scope.
	reasonScope = "scope"
)

// New loads saved state, reads Claude Code's files, and builds the first
// snapshot. It never uses the network.
//
// Parameters:
//   - ctx: scope for the startup reads and logs.
//   - opt: the engine settings.
//   - deps: the collaborators.
//
// Returns:
//   - *Engine: the engine, ready to Run.
func New(ctx context.Context, opt Options, deps Deps) *Engine {
	if deps.Logger == nil {
		deps.Logger = nopLogger{}
	}

	now := time.Now().Round(0)

	engine := &Engine{
		opt: opt, deps: deps, log: deps.Logger, refresh: make(chan struct{}, 1),
		mu: sync.Mutex{}, snapshot: "", publish: nil,
		state: state.Fresh(), usage: usage.Usage{Bars: nil, Extra: nil}, startedAt: now,
		account: "", oauth: claudecode.OAuth{}, credErr: nil, active: false,
		decision: schedule.Decision{}, authBadSince: time.Time{}, dirty: false,
	}

	engine.restore(ctx)
	engine.observe(ctx, now)

	engine.decision = engine.decide(now, false)
	engine.snapshot = engine.buildSnapshot()

	return engine
}

// RequestRefresh asks for an early fetch.
//
// Requests coalesce, and the policy ignores them while fetching would be too
// soon.
func (e *Engine) RequestRefresh() {
	select {
	case e.refresh <- struct{}{}:
	default:
	}
}

// Run drives the engine until ctx is canceled, then saves the state.
//
// Timers stop while the machine sleeps, so each wait is capped at the probe
// interval and every decision uses wall-clock time.
//
// Parameters:
//   - ctx: cancellation for the loop.
func (e *Engine) Run(ctx context.Context) {
	manual := false

	for {
		e.step(ctx, time.Now().Round(0), manual)

		manual = false

		wait := min(max(time.Until(e.decision.WakeAt), minWait), e.opt.Policy.Probe)
		timer := time.NewTimer(wait)

		select {
		case <-ctx.Done():
			timer.Stop()
			e.save(context.WithoutCancel(ctx))

			return
		case <-e.refresh:
			timer.Stop()

			manual = true
		case <-timer.C:
		}
	}
}

// SetPublisher sets the function that receives every changed snapshot.
//
// Parameters:
//   - publish: the receiver, called from the engine goroutine.
func (e *Engine) SetPublisher(publish func(string)) {
	e.mu.Lock()

	e.publish = publish
	e.mu.Unlock()
}

// Snapshot returns the current snapshot JSON.
//
// Returns:
//   - string: the snapshot.
func (e *Engine) Snapshot() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.snapshot
}

// adoptCache takes Claude Code's cached usage when it is newer than the data.
//
// Parameters:
//   - ctx: scope for logs.
//   - global: Claude Code's global state.
//   - now: the current time, which bounds future-dated caches.
func (e *Engine) adoptCache(ctx context.Context, global claudecode.GlobalState, now time.Time) {
	cache, ok := global.UsableCache()
	if !ok || !cache.FetchedAt.After(e.dataAt()) || cache.FetchedAt.After(now.Add(clockSkew)) {
		return
	}

	parsed, err := usage.Parse(cache.Payload)
	if err != nil {
		return
	}

	e.setData(parsed, cache.Payload, cache.FetchedAt, sourceClaudeCode, cache.AccountUUID)
	e.log.Debug(ctx, "Adopted Claude Code's cached usage", logKeyAge, now.Sub(cache.FetchedAt).Round(time.Second))
}

// alert sends threshold and login alerts.
//
// Parameters:
//   - ctx: cancellation for the sends.
//   - now: the current time.
func (e *Engine) alert(ctx context.Context, now time.Time) {
	if e.deps.Notifier == nil {
		return
	}

	if e.state.Data != nil {
		for _, pending := range e.state.Alerts.Evaluate(now, e.usage.Bars, e.opt.Thresholds) {
			message := notify.Format(pending, now)

			message.ReplacesID = e.state.Alerts.ReplaceIDs[pending.Bar.ID]

			id, err := e.deps.Notifier.Send(ctx, message)
			if err != nil {
				e.log.Warn(ctx, "Notification failed", logKeyErr, err)

				continue
			}

			e.state.Alerts.Record(pending, id, now)

			e.dirty = true
		}
	}

	e.authAlert(ctx, now)
}

// authAlert raises one notification per login problem that lasts a while.
//
// An expiring token is not a problem, because Claude Code refreshes it when
// it is used.
//
// Parameters:
//   - ctx: cancellation for the send.
//   - now: the current time.
func (e *Engine) authAlert(ctx context.Context, now time.Time) {
	status, _ := e.status()
	if status != StatusLoggedOut && status != StatusAuthError {
		e.authBadSince = time.Time{}

		if e.state.Alerts.Auth != "" {
			e.state.Alerts.Auth, e.dirty = "", true
		}

		return
	}

	if e.authBadSince.IsZero() {
		e.authBadSince = now
	}

	if !e.opt.NotifyAuth || now.Sub(e.authBadSince) < authAlertAfter {
		return
	}

	key := string(status) + "|" + strconv.FormatInt(millis(e.oauth.ExpiresAt), 10)
	if e.state.Alerts.Auth == key {
		return
	}

	_, err := e.deps.Notifier.Send(ctx, notify.AuthMessage())
	if err != nil {
		e.log.Warn(ctx, "Notification failed", logKeyErr, err)

		return
	}

	e.state.Alerts.Auth, e.dirty = key, true
}

// buildSnapshot renders the current state.
//
// Returns:
//   - string: the snapshot JSON.
func (e *Engine) buildSnapshot() string {
	status, message := e.status()

	snap := snapshot{
		V:                  SnapshotVersion,
		Status:             status,
		Message:            message,
		Mode:               e.opt.Mode,
		Source:             "",
		Active:             e.active,
		Plan:               planName(e.oauth.SubscriptionType, e.oauth.RateLimitTier),
		FetchedAtMs:        0,
		NextUpdateAtMs:     millis(e.decision.NextFetchAt),
		RefreshAllowedAtMs: nil,
		LoginExpiresAtMs:   millis(e.oauth.LoginExpiresAt),
		WarnAt:             e.opt.Warn,
		CritAt:             e.opt.Crit,
		Bars:               barsJSON(e.usage.Bars, e.opt.Warn, e.opt.Crit),
		ExtraUsage:         extraToJSON(e.usage.Extra, e.opt.Warn, e.opt.Crit),
	}

	if data := e.state.Data; data != nil {
		snap.Source, snap.FetchedAtMs = data.Source, millis(data.FetchedAt)
	}

	if allowed := e.decision.RefreshAllowedAt; !allowed.IsZero() {
		snap.RefreshAllowedAtMs = new(millis(allowed))
	}

	return encodeSnapshot(&snap)
}

// dataAt returns when the current data was fetched.
//
// Returns:
//   - [time.Time]: the fetch time, or zero without data.
func (e *Engine) dataAt() time.Time {
	if e.state.Data == nil {
		return time.Time{}
	}

	return e.state.Data.FetchedAt
}

// decide runs the policy on the current state.
//
// Parameters:
//   - now: the current time.
//   - manual: whether the widget asked for a refresh.
//
// Returns:
//   - schedule.Decision: the policy's decision.
func (e *Engine) decide(now time.Time, manual bool) schedule.Decision {
	return schedule.Decide(e.opt.Policy, &schedule.Inputs{
		Now:         now,
		CacheOnly:   e.opt.Mode == config.ModeCacheOnly,
		Active:      e.active,
		CredErr:     e.credErr,
		OAuth:       e.oauth,
		DataAt:      e.dataAt(),
		LastAttempt: e.state.LastAttempt,
		StartedAt:   e.startedAt,
		Backoff:     e.state.Backoff,
		AuthBlock:   e.state.AuthBlock,
		NextReset:   e.nextReset(),
		Manual:      manual,
	})
}

// fetch calls the usage endpoint and records the outcome.
//
// Parameters:
//   - ctx: cancellation for the request.
//   - now: the attempt time.
func (e *Engine) fetch(ctx context.Context, now time.Time) {
	if e.deps.Fetcher == nil {
		return
	}

	e.state.LastAttempt, e.dirty = now, true

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	body, err := e.deps.Fetcher.Fetch(fetchCtx, e.oauth.AccessToken.Reveal())

	if ctx.Err() != nil {
		return
	}

	if err != nil {
		e.fetchFailed(ctx, err, now)

		return
	}

	parsed, err := usage.Parse(body)
	if err != nil {
		e.state.Backoff = e.state.Backoff.Next(e.opt.Policy, schedule.BackoffBadPayload, now, 0)
		e.log.Warn(ctx, "Unrecognized usage payload",
			logKeyErr, err,
			logKeyRetryAt, e.state.Backoff.Until)

		return
	}

	e.setData(parsed, body, now, sourceAPI, e.account)

	e.state.Backoff, e.state.AuthBlock = schedule.Backoff{}, nil
	e.log.Debug(ctx, "Fetched usage", logKeyBars, len(parsed.Bars))
}

// fetchFailed records a failed fetch as an auth block or a backoff.
//
// Parameters:
//   - ctx: scope for logs.
//   - err: the fetch error.
//   - now: the attempt time.
func (e *Engine) fetchFailed(ctx context.Context, err error, now time.Time) {
	fetchErr, ok := errors.AsType[*usage.FetchError](err)
	if !ok {
		fetchErr = &usage.FetchError{Kind: usage.KindNetwork, Status: 0, RetryAfter: 0, Detail: "", Err: err}
	}

	kind := schedule.BackoffNetwork

	switch fetchErr.Kind {
	case usage.KindUnauthorized, usage.KindScope:
		reason := reasonUnauthorized
		if fetchErr.Kind == usage.KindScope {
			reason = reasonScope
		}

		e.state.AuthBlock = &schedule.AuthBlock{TokenExpiresAt: e.oauth.ExpiresAt, Reason: reason, Since: now}
		e.log.Warn(ctx, "Usage endpoint rejected the login", logKeyErr, fetchErr)

		return
	case usage.KindRateLimited:
		kind = schedule.BackoffRateLimited
	case usage.KindServer:
		kind = schedule.BackoffServer
	case usage.KindNetwork:
	default:
	}

	e.state.Backoff = e.state.Backoff.Next(e.opt.Policy, kind, now, fetchErr.RetryAfter)
	e.log.Warn(ctx, "Usage fetch failed",
		logKeyErr, fetchErr,
		logKeyRetryAt, e.state.Backoff.Until)
}

// nextReset returns the earliest reset after the current data was fetched.
//
// Returns:
//   - [time.Time]: the reset, or zero.
func (e *Engine) nextReset() time.Time {
	var next time.Time

	fetched := e.dataAt()

	for _, bar := range e.usage.Bars {
		if !bar.ResetsAt.IsZero() && bar.ResetsAt.After(fetched) {
			next = schedule.Earliest(next, bar.ResetsAt)
		}
	}

	return next
}

// observe reads activity, Claude Code's cache, and, in hybrid mode, the login.
//
// Parameters:
//   - ctx: scope for logs.
//   - now: the current time.
func (e *Engine) observe(ctx context.Context, now time.Time) {
	if e.deps.Activity != nil {
		e.active = e.deps.Activity.Active(now)
	}

	if e.deps.Global != nil {
		global, err := e.deps.Global.Read()
		if err != nil {
			e.log.Debug(ctx, "Reading Claude Code's global config failed", logKeyErr, err)
		}

		e.account = global.AccountUUID

		data := e.state.Data
		if data != nil && data.AccountUUID != "" && global.AccountUUID != "" && data.AccountUUID != global.AccountUUID {
			e.log.Info(ctx, "Signed-in account changed, dropping old data")

			e.state.Data, e.usage, e.dirty = nil, usage.Usage{Bars: nil, Extra: nil}, true
		}

		e.adoptCache(ctx, global, now)
	}

	if e.opt.Mode != config.ModeCacheOnly && e.deps.Credentials != nil {
		e.oauth, e.credErr = e.deps.Credentials.Read()
	}
}

// publishSnapshot sends the snapshot to the publisher when it changed.
func (e *Engine) publishSnapshot() {
	snap := e.buildSnapshot()

	e.mu.Lock()

	changed := snap != e.snapshot

	e.snapshot = snap

	publish := e.publish
	e.mu.Unlock()

	if changed && publish != nil {
		publish(snap)
	}
}

// restore loads the saved state and re-parses its payload.
//
// Parameters:
//   - ctx: scope for logs.
func (e *Engine) restore(ctx context.Context) {
	loaded, err := e.deps.Store.Load()
	if err != nil {
		e.log.Warn(ctx, "Discarding saved state", logKeyErr, err)
	}

	e.state = loaded

	data := e.state.Data
	if data == nil {
		return
	}

	parsed, err := usage.Parse(data.Payload)
	if err != nil {
		e.log.Warn(ctx, "Dropping saved data the parser no longer accepts", logKeyErr, err)

		e.state.Data = nil

		return
	}

	e.usage = parsed
}

// save persists the state.
//
// Parameters:
//   - ctx: scope for logs.
func (e *Engine) save(ctx context.Context) {
	err := e.deps.Store.Save(e.state)
	if err != nil {
		e.log.Warn(ctx, "Saving state failed", logKeyErr, err)

		return
	}

	e.dirty = false
}

// setData replaces the current data.
//
// Parameters:
//   - parsed: the parsed payload.
//   - payload: the raw payload.
//   - fetched: when it was fetched.
//   - source: api or claude-code.
//   - account: the account it belongs to.
func (e *Engine) setData(parsed usage.Usage, payload []byte, fetched time.Time, source, account string) {
	e.usage = parsed
	e.state.Data = &state.Data{
		Payload:     append(json.RawMessage(nil), payload...),
		FetchedAt:   fetched,
		Source:      source,
		AccountUUID: account,
	}
	e.dirty = true
}

// status maps the current state to the published status.
//
// Returns:
//   - Status: the status.
//   - string: the explanation.
func (e *Engine) status() (Status, string) {
	return statusFor(e.opt.Mode, e.decision.Gate, e.state.Backoff, e.state.Data)
}

// step runs one wake: observe, decide, fetch, alert, publish, and save.
//
// Parameters:
//   - ctx: cancellation for network and notification calls.
//   - now: the wake time.
//   - manual: whether the widget asked for a refresh.
func (e *Engine) step(ctx context.Context, now time.Time, manual bool) {
	e.observe(ctx, now)

	decision := e.decide(now, manual)
	if decision.Fetch {
		e.fetch(ctx, now)

		decision = e.decide(now, false)
	}

	e.decision = decision
	e.alert(ctx, now)
	e.publishSnapshot()

	if e.dirty {
		e.save(ctx)
	}
}
