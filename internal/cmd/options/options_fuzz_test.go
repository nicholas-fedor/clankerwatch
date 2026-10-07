// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package options

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/clankerwatch/internal/config"
)

// FuzzResolveSettings feeds random environment values for every variable.
//
// Resolution must never panic. A success must be a valid configuration, and
// a failure must wrap ErrInvalidSetting. The mask selects which variables are
// set, so unset and empty stay distinct.
func FuzzResolveSettings(f *testing.F) {
	f.Add(uint8(0), "", "", "", "", "", "", "", "")
	f.Add(uint8(0xff), "cache-only", "3m", "30m", "50,90", "false", "debug", "/claude", "/state")
	f.Add(uint8(0xff), "hybrid", "2m", "2m", "", "1", "ERROR", "", "")
	f.Add(uint8(0x0f), "bogus", "-5m", "1h", "0,101", "maybe", "loud", "x", "y")
	f.Add(uint8(0x06), "", "9223372036854775807ns", "1ns", "nan", "", "info+2", "", "")

	f.Fuzz(func(t *testing.T, mask uint8, mode, interval, idle, notify, auth, level, claudeDir, stateDir string) {
		values := []string{mode, interval, idle, notify, auth, level, claudeDir, stateDir}
		env := map[string]string{}

		for index, item := range settings {
			if mask&(1<<index) != 0 {
				env[item.env] = values[index]
			}
		}

		root := &cobra.Command{Use: Name}
		BindPersistent(root, New())

		cfg, err := ResolveSettings(root.PersistentFlags(), envMap(env), testDirs)
		if err != nil {
			require.ErrorIs(t, err, ErrInvalidSetting)
			assert.Equal(t, config.Config{}, cfg)

			return
		}

		assert.NoError(t, cfg.Validate())
	})
}
