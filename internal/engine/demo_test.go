// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/engine/mocks"
	"github.com/nicholas-fedor/clankerwatch/internal/state"
	"github.com/nicholas-fedor/clankerwatch/internal/usage"
)

// sceneScript returns a demo script frozen in the given scene.
//
// Parameters:
//   - scene: the scene index, which may exceed the scene count.
//
// Returns:
//   - *demoScript: the script.
func sceneScript(scene int) *demoScript {
	now := testNow.Add(time.Duration(scene)*demoStep + time.Second)

	return &demoScript{start: testNow, now: func() time.Time { return now }}
}

// TestDemoKeepsThresholdsAndUsesAFastSchedule checks what Demo changes and keeps.
func TestDemoKeepsThresholdsAndUsesAFastSchedule(t *testing.T) {
	t.Parallel()

	base := testOptions()
	base.Mode = config.ModeCacheOnly
	base.Thresholds = []float64{50, 90}
	notifier := mocks.NewMockNotifier(t)
	logger := mocks.NewMockLogger(t)

	opt, deps := Demo(base, notifier, logger)

	assert.Equal(t, config.ModeHybrid, opt.Mode)
	assert.Equal(t, []float64{50, 90}, opt.Thresholds)
	assert.InDelta(t, base.Warn, opt.Warn, 0.001)
	assert.Equal(t, demoInterval, opt.Policy.Interval)
	assert.Equal(t, demoProbe, opt.Policy.Probe)
	assert.Zero(t, opt.Policy.StartupDelay)

	assert.IsType(t, &demoScript{}, deps.Credentials)
	assert.Same(t, deps.Credentials, deps.Fetcher)
	assert.Nil(t, deps.Global)
	assert.Equal(t, alwaysActive{}, deps.Activity)
	assert.IsType(t, &state.Memory{}, deps.Store)
	assert.Same(t, notifier, deps.Notifier)
	assert.Same(t, logger, deps.Logger)
	assert.True(t, alwaysActive{}.Active(testNow))
}

// TestDemoScriptCyclesThroughScenes checks the scene follows the clock and wraps.
func TestDemoScriptCyclesThroughScenes(t *testing.T) {
	t.Parallel()

	for index := range 2 * len(demoScenes) {
		assert.Equal(t, demoScenes[index%len(demoScenes)], sceneScript(index).scene(), "step %d", index)
	}
}

// TestDemoScriptRead returns the scene's login.
func TestDemoScriptRead(t *testing.T) {
	t.Parallel()

	login, err := sceneScript(0).Read()
	require.NoError(t, err)
	assert.Equal(t, "demo", login.AccessToken.Reveal())
	assert.Equal(t, testNow.Add(demoTokenLife), login.ExpiresAt)
	assert.Equal(t, "Pro", planName(login.SubscriptionType, login.RateLimitTier))
	assert.True(t, login.HasScope("user:profile"))

	expiring := sceneScript(5)
	login, err = expiring.Read()
	require.NoError(t, err)
	assert.Equal(t, expiring.now().Add(demoExpiringIn), login.ExpiresAt)

	_, err = sceneScript(6).Read()
	require.ErrorIs(t, err, claudecode.ErrLoggedOut)
}

// TestDemoScriptFetch returns the scene's payload or a rate limit.
func TestDemoScriptFetch(t *testing.T) {
	t.Parallel()

	body, err := sceneScript(2).Fetch(t.Context(), "demo")
	require.NoError(t, err)

	parsed, err := usage.Parse(body)
	require.NoError(t, err)
	require.Len(t, parsed.Bars, 3)
	assert.InDelta(t, 97.0, parsed.Bars[0].Percent, 0.001)
	assert.Equal(t, "critical", parsed.Bars[0].Severity)
	assert.Equal(t, "warning", parsed.Bars[1].Severity)
	assert.Equal(t, "normal", parsed.Bars[2].Severity)
	assert.Equal(t, "Current week (Fable)", parsed.Bars[2].Label)
	assert.Nil(t, parsed.Extra)

	body, err = sceneScript(3).Fetch(t.Context(), "demo")
	require.NoError(t, err)

	parsed, err = usage.Parse(body)
	require.NoError(t, err)
	require.NotNil(t, parsed.Extra)
	assert.Equal(t, "$12.50", parsed.Extra.Used.String())

	_, err = sceneScript(4).Fetch(t.Context(), "demo")

	var fetchErr *usage.FetchError

	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, usage.KindRateLimited, fetchErr.Kind)
	assert.Equal(t, http.StatusTooManyRequests, fetchErr.Status)
	assert.Equal(t, demoRetryAfter, fetchErr.RetryAfter)
}

// TestDemoPayloadIsValidJSON checks every scene renders a payload the parser accepts.
func TestDemoPayloadIsValidJSON(t *testing.T) {
	t.Parallel()

	for index, scene := range demoScenes {
		body := demoPayload(testNow, scene)

		assert.True(t, json.Valid(body), "scene %d", index)

		_, err := usage.Parse(body)
		require.NoError(t, err, "scene %d", index)
	}
}

// TestDemoDrivesTheEngineThroughEveryStatus runs the real engine on the demo script.
//
// One full cycle of scenes must reach each status the demo exists to show.
func TestDemoDrivesTheEngineThroughEveryStatus(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		opt, deps := Demo(testOptions(), nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		e := New(ctx, opt, deps)

		var (
			mu   sync.Mutex
			seen = map[Status]bool{}
		)

		e.SetPublisher(func(snap string) {
			var view struct {
				Status Status `json:"status"`
			}

			if json.Unmarshal([]byte(snap), &view) == nil {
				mu.Lock()
				seen[view.Status] = true
				mu.Unlock()
			}
		})

		done := make(chan struct{})

		go func() {
			defer close(done)

			e.Run(ctx)
		}()

		synctest.Sleep(time.Duration(len(demoScenes)) * demoStep)
		cancel()
		<-done

		mu.Lock()
		defer mu.Unlock()

		for _, status := range []Status{StatusOK, StatusRateLimited, StatusTokenExpired, StatusLoggedOut} {
			assert.True(t, seen[status], "status %s", status)
		}
	})
}
