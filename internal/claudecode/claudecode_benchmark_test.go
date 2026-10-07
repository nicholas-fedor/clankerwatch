// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package claudecode_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nicholas-fedor/clankerwatch/internal/claudecode"
)

// benchmarkSessions is how many session files the activity benchmark scans.
const benchmarkSessions = 20

// BenchmarkCredentialsReaderRead measures the poll path on an unchanged
// credentials file, which costs one stat and no read.
func BenchmarkCredentialsReaderRead(b *testing.B) {
	path := filepath.Join(b.TempDir(), ".credentials.json")
	content := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-bench","expiresAt":1784000000000,` +
		`"refreshToken":"sk-ant-ort01-bench","scopes":["user:inference","user:profile"],"subscriptionType":"max"}}`

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		b.Fatal(err)
	}

	reader := claudecode.NewCredentialsReader(path)

	var oauth claudecode.OAuth

	for b.Loop() {
		oauth, err = reader.Read()
		if err != nil {
			b.Fatal(err)
		}
	}

	if oauth.AccessToken.Reveal() == "" {
		b.Fatal("missing token")
	}
}

// BenchmarkActivityProbeActive measures one poll over a sessions directory
// with idle, unchanged session files and their key files.
func BenchmarkActivityProbeActive(b *testing.B) {
	dir := b.TempDir()
	stale := time.Now().Add(-time.Hour)

	for i := range benchmarkSessions {
		pid := strconv.Itoa(1_000_000 + i)
		session := filepath.Join(dir, pid+".json")

		err := os.WriteFile(session, []byte(`{"status":"idle","pid":`+pid+`}`), 0o600)
		if err == nil {
			err = os.Chtimes(session, stale, stale)
		}

		if err == nil {
			err = os.WriteFile(filepath.Join(dir, pid+".abc.key"), []byte("secret"), 0o600)
		}

		if err != nil {
			b.Fatal(err)
		}
	}

	probe := claudecode.NewActivityProbe(dir, time.Minute)
	now := time.Now()

	var active bool

	for b.Loop() {
		active = probe.Active(now)
	}

	if active {
		b.Fatal("idle sessions reported active")
	}
}
