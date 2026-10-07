// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import (
	"testing"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
)

// fuzzBase anchors the fuzzed offsets, so every generated time is far from
// the zero time.
var fuzzBase = time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)

// fuzzKinds are the backoff kinds a fuzzed selector picks from.
var fuzzKinds = []BackoffKind{BackoffRateLimited, BackoffServer, BackoffBadPayload, BackoffNetwork, "unknown"}

// fuzzTime turns a fuzzed offset into a time, with 0 meaning the zero time.
//
// Parameters:
//   - offset: seconds from fuzzBase, or 0.
//
// Returns:
//   - [time.Time]: the time, or zero.
func fuzzTime(offset int32) time.Time {
	if offset == 0 {
		return time.Time{}
	}

	return fuzzBase.Add(time.Duration(offset) * time.Second)
}

// FuzzDecide checks the decision invariants on arbitrary inputs.
//
// The scheduler runs on state restored from disk and on whatever the wall
// clock says after a suspend, so any combination of times must give a
// coherent decision: a fetch only when nothing gates it, a wake time that is
// always set, a planned fetch in the future when no fetch happens now, and a
// backoff decision that plans the fetch for when the backoff ends.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzDecide(f *testing.F) {
	f.Add(int32(3600), int32(3540), int32(0), int32(0), int32(0), int32(0), int32(28800), uint8(0), uint8(0), true, false, false)
	f.Add(int32(3600), int32(0), int32(0), int32(0), int32(0), int32(4200), int32(28800), uint8(0), uint8(0), true, true, false)
	f.Add(int32(5), int32(0), int32(0), int32(0), int32(0), int32(0), int32(28800), uint8(0), uint8(0), true, false, false)
	f.Add(int32(3600), int32(3000), int32(3000), int32(3500), int32(0), int32(0), int32(28800), uint8(0), uint8(0), false, false, false)
	f.Add(int32(3600), int32(3000), int32(0), int32(0), int32(3000), int32(0), int32(28800), uint8(1), uint8(1), true, false, false)
	f.Add(int32(3600), int32(3000), int32(0), int32(0), int32(0), int32(0), int32(3700), uint8(2), uint8(2), true, false, true)
	f.Add(int32(-100), int32(500), int32(900), int32(-50), int32(-7), int32(-1), int32(-30), uint8(5), uint8(7), false, true, false)

	f.Fuzz(func(
		t *testing.T,
		now, dataAt, lastAttempt, nextReset, blockSince, backoffUntil, expiresAt int32,
		credSel, loginSel uint8,
		active, manual, cacheOnly bool,
	) {
		in := &Inputs{
			Now:         fuzzBase.Add(time.Duration(now) * time.Second),
			DataAt:      fuzzTime(dataAt),
			LastAttempt: fuzzTime(lastAttempt),
			StartedAt:   fuzzBase,
			NextReset:   fuzzTime(nextReset),
			Backoff:     Backoff{Until: fuzzTime(backoffUntil), Kind: fuzzKinds[int(loginSel)%len(fuzzKinds)], Consecutive: 1},
			OAuth: claudecode.OAuth{
				AccessToken: "token",
				ExpiresAt:   fuzzTime(expiresAt),
				Scopes:      []string{profileScope},
			},
			Active:    active,
			Manual:    manual,
			CacheOnly: cacheOnly,
		}

		switch credSel % 4 {
		case 1:
			in.CredErr = claudecode.ErrNoCredentials
		case 2:
			in.CredErr = claudecode.ErrLoggedOut
		case 3:
			in.CredErr = errUnreadable
		}

		if loginSel&0x8 != 0 {
			in.OAuth.Scopes = nil
		}

		if loginSel&0x10 != 0 {
			in.OAuth.AccessToken = ""
		}

		if blockSince != 0 {
			in.AuthBlock = &AuthBlock{TokenExpiresAt: in.OAuth.ExpiresAt, Since: fuzzTime(blockSince), Reason: "unauthorized"}
			if loginSel&0x20 != 0 {
				in.AuthBlock.TokenExpiresAt = in.AuthBlock.TokenExpiresAt.Add(time.Second)
			}
		}

		got := Decide(testPolicy, in)

		if got.Fetch && got.Gate != GateNone {
			t.Fatalf("fetch with gate %d", got.Gate)
		}

		if got.WakeAt.IsZero() {
			t.Fatal("wake time is zero")
		}

		if got.WakeAt.After(in.Now.Add(testPolicy.Probe)) {
			t.Fatalf("wake %v is after the probe interval from %v", got.WakeAt, in.Now)
		}

		if in.CacheOnly && got.Fetch {
			t.Fatal("cache-only decision fetches")
		}

		if !in.CacheOnly && !got.Fetch && got.Gate == GateNone && !got.NextFetchAt.After(in.Now) {
			t.Fatalf("no fetch, no gate, and the planned fetch %v is not after %v", got.NextFetchAt, in.Now)
		}

		if got.Gate == GateBackoff && !got.NextFetchAt.Equal(in.Backoff.Until) {
			t.Fatalf("backoff plans the fetch for %v, want %v", got.NextFetchAt, in.Backoff.Until)
		}
	})
}

// FuzzBackoffNext checks the backoff invariants on arbitrary failures.
//
// The previous backoff comes from the state file, and Retry-After comes from
// the server, so neither can be trusted. The delay must stay within the
// kind's cap, and the failure count must continue only a run of the same
// kind.
//
// Parameters:
//   - f: fuzzing handle.
func FuzzBackoffNext(f *testing.F) {
	f.Add(uint8(0), uint8(0), int32(0), int32(0), int64(0))
	f.Add(uint8(0), uint8(0), int32(3), int32(-600), int64(600*time.Second))
	f.Add(uint8(1), uint8(3), int32(10), int32(-60), int64(0))
	f.Add(uint8(0), uint8(0), int32(1), int32(-60), int64(72*time.Hour))
	f.Add(uint8(4), uint8(2), int32(-5), int32(0), int64(-time.Hour))

	f.Fuzz(func(t *testing.T, previousSel, kindSel uint8, consecutive, sinceOffset int32, retryAfter int64) {
		now := fuzzBase
		previous := Backoff{
			Until:       now,
			Since:       now.Add(time.Duration(sinceOffset) * time.Second),
			Kind:        fuzzKinds[int(previousSel)%len(fuzzKinds)],
			Consecutive: int(consecutive),
		}
		kind := fuzzKinds[int(kindSel)%len(fuzzKinds)]

		got := previous.Next(testPolicy, kind, now, time.Duration(retryAfter))

		_, ceiling := testPolicy.curve(kind)
		wait := got.Until.Sub(now)

		if wait < 0 || wait > ceiling {
			t.Fatalf("delay %v is outside [0, %v]", wait, ceiling)
		}

		if got.Kind != kind {
			t.Fatalf("kind %q, want %q", got.Kind, kind)
		}

		if previous.Kind == kind && previous.Consecutive > 0 {
			if got.Consecutive != previous.Consecutive+1 || !got.Since.Equal(previous.Since) {
				t.Fatalf("run not continued: %+v after %+v", got, previous)
			}

			return
		}

		if got.Consecutive != 1 || !got.Since.Equal(now) {
			t.Fatalf("run not restarted: %+v after %+v", got, previous)
		}
	})
}
