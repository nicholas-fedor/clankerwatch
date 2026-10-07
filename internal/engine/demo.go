// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/schedule"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// demoScene is one state the demo shows for demoStep.
type demoScene struct {
	session, weekly, fable float64
	extra                  bool
	rateLimited            bool
	expiring               bool
	loggedOut              bool
}

// demoScript stands in for Claude Code's login and the usage endpoint.
type demoScript struct {
	start time.Time
	now   func() time.Time
}

// alwaysActive reports Claude Code as active.
type alwaysActive struct{}

// Demo schedule and canned values.
const (
	demoStep          = 12 * time.Second
	demoExpirySkew    = 5 * time.Minute
	demoScope         = "user:profile"
	demoResetsAt      = "resets_at"
	demoDecimals      = 2
	demoInterval      = 4 * time.Second
	demoProbe         = 2 * time.Second
	demoManualGap     = time.Second
	demoResetGrace    = time.Second
	demoAuthRetry     = 5 * time.Second
	demoBackoffBase   = 2 * time.Second
	demoRateLimitBase = 3 * time.Second
	demoBackoffCap    = 10 * time.Second
	demoRetryAfter    = 8 * time.Second
	demoTokenLife     = 8 * time.Hour
	demoLoginLife     = 30 * 24 * time.Hour
	demoExpiringIn    = time.Minute
	demoSessionReset  = 72 * time.Minute
	demoWeeklyReset   = 6 * 24 * time.Hour
	demoMonthlyLimit  = 5000
	demoUsedCredits   = 1250
	demoWarnSeverity  = 80
	demoCritSeverity  = 95
)

// demoScenes cycles through every state the widget shows.
//
//nolint:mnd // The percentages are canned demo values.
var demoScenes = []demoScene{
	{session: 66, weekly: 17, fable: 0, extra: false, rateLimited: false, expiring: false, loggedOut: false},
	{session: 84, weekly: 41, fable: 5, extra: false, rateLimited: false, expiring: false, loggedOut: false},
	{session: 97, weekly: 88, fable: 12, extra: false, rateLimited: false, expiring: false, loggedOut: false},
	{session: 66, weekly: 17, fable: 0, extra: true, rateLimited: false, expiring: false, loggedOut: false},
	{session: 0, weekly: 0, fable: 0, extra: false, rateLimited: true, expiring: false, loggedOut: false},
	{session: 0, weekly: 0, fable: 0, extra: false, rateLimited: false, expiring: true, loggedOut: false},
	{session: 0, weekly: 0, fable: 0, extra: false, rateLimited: false, expiring: false, loggedOut: true},
}

// Demo returns options and dependencies that replay canned usage through the
// real engine, cycling through every state the widget shows.
//
// It reads no files, uses no network, and keeps its state in memory.
//
// Parameters:
//   - opt: the base options, whose thresholds and colors are kept.
//   - notifier: the alert sender, or nil.
//   - logger: the logger.
//
// Returns:
//   - Options: options with a fast schedule.
//   - Deps: the scripted collaborators.
func Demo(opt Options, notifier Notifier, logger Logger) (Options, Deps) {
	opt.Mode = config.ModeHybrid
	opt.Policy = schedule.Policy{
		Interval:        demoInterval,
		IdleInterval:    demoInterval,
		Probe:           demoProbe,
		ManualGap:       demoManualGap,
		ExpirySkew:      demoExpirySkew,
		ResetGrace:      demoResetGrace,
		AuthRetry:       demoAuthRetry,
		StartupDelay:    0,
		RateLimitBase:   demoRateLimitBase,
		RateLimitCap:    demoBackoffCap,
		ServerErrorBase: demoBackoffBase,
		ServerErrorCap:  demoBackoffCap,
		NetworkBase:     demoBackoffBase,
		NetworkCap:      demoBackoffCap,
	}

	script := &demoScript{start: time.Now(), now: time.Now}

	return opt, Deps{
		Credentials: script,
		Global:      nil,
		Activity:    alwaysActive{},
		Fetcher:     script,
		Notifier:    notifier,
		Store:       new(state.Memory),
		Logger:      logger,
	}
}

// Active always reports activity.
//
// Returns:
//   - bool: true.
func (alwaysActive) Active(time.Time) bool { return true }

// Fetch returns the current scene's payload or a rate limit.
//
// Returns:
//   - []byte: the payload.
//   - error: a rate-limit FetchError in the rate-limited scene.
func (s *demoScript) Fetch(context.Context, string) ([]byte, error) {
	scene := s.scene()
	if scene.rateLimited {
		return nil, &usage.FetchError{
			Kind:       usage.KindRateLimited,
			Status:     http.StatusTooManyRequests,
			RetryAfter: demoRetryAfter,
			Detail:     "",
			Err:        nil,
		}
	}

	return demoPayload(s.start, scene), nil
}

// Read returns the current scene's login.
//
// Returns:
//   - claudecode.OAuth: a Pro login, expiring soon in the paused scene.
//   - error: claudecode.ErrLoggedOut in the signed-out scene.
func (s *demoScript) Read() (claudecode.OAuth, error) {
	scene := s.scene()
	if scene.loggedOut {
		return claudecode.OAuth{}, claudecode.ErrLoggedOut
	}

	login := claudecode.OAuth{
		AccessToken:      "demo",
		ExpiresAt:        s.start.Add(demoTokenLife),
		LoginExpiresAt:   s.start.Add(demoLoginLife),
		Scopes:           []string{demoScope},
		SubscriptionType: "pro",
		RateLimitTier:    "default_claude_pro",
	}
	if scene.expiring {
		login.ExpiresAt = s.now().Add(demoExpiringIn)
	}

	return login, nil
}

// scene returns the scene for the current time.
//
// Returns:
//   - demoScene: the scene.
func (s *demoScript) scene() demoScene {
	return demoScenes[int(s.now().Sub(s.start)/demoStep)%len(demoScenes)]
}

// demoPayload renders a scene as a usage payload.
//
// Parameters:
//   - start: the demo start, which anchors the reset times.
//   - scene: the scene.
//
// Returns:
//   - []byte: the payload.
func demoPayload(start time.Time, scene demoScene) []byte {
	at := func(offset time.Duration) string { return start.Add(offset).UTC().Format(time.RFC3339Nano) }
	severity := func(percent float64) string {
		switch {
		case percent >= demoCritSeverity:
			return "critical"
		case percent >= demoWarnSeverity:
			return "warning"
		default:
			return "normal"
		}
	}

	session, week := at(demoSessionReset), at(demoWeeklyReset)
	body := map[string]any{
		"five_hour": map[string]any{"utilization": scene.session, demoResetsAt: session},
		"seven_day": map[string]any{"utilization": scene.weekly, demoResetsAt: week},
		"limits": []map[string]any{
			{
				"kind": "session", "group": "session", "percent": scene.session,
				"severity": severity(scene.session), demoResetsAt: session,
			},
			{
				"kind": "weekly_all", "group": "weekly", "percent": scene.weekly,
				"severity": severity(scene.weekly), demoResetsAt: week, "is_active": true,
			},
			{
				"kind": "weekly_scoped", "group": "weekly", "percent": scene.fable, "severity": "normal",
				demoResetsAt: week, "scope": map[string]any{"model": map[string]any{"display_name": "Fable"}},
			},
		},
		"extra_usage": map[string]any{
			"is_enabled": scene.extra, "monthly_limit": demoMonthlyLimit, "used_credits": demoUsedCredits,
			"currency": "USD", "decimal_places": demoDecimals,
		},
	}

	content, err := json.Marshal(body)
	if err != nil {
		// The payload holds only plain values, so encoding cannot fail.
		panic(err)
	}

	return content
}
