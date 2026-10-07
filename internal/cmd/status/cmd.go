// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package status

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/clankerwatch/internal/cmd/options"
	"github.com/nicholas-fedor/clankerwatch/internal/config"
	"github.com/nicholas-fedor/clankerwatch/internal/daemon"
)

// Reporter builds the status report.
type Reporter interface {
	// Status reports the settings and the snapshot the daemon would publish.
	//
	// Parameters:
	//   - ctx: scope for the reads.
	//   - cfg: the settings.
	//
	// Returns:
	//   - daemon.Report: the report.
	Status(ctx context.Context, cfg config.Config) daemon.Report
}

const (
	// flagJSON is the JSON output flag.
	flagJSON = "json"

	// tabPadding separates the report's columns.
	tabPadding = 2
)

// NewCommand returns the status command.
//
// Parameters:
//   - ctx: scope for the reads.
//   - reporter: builds the report.
//   - resolve: resolves the settings.
//   - stdout: stream the report is written to.
//
// Returns:
//   - *cobra.Command: the status command.
func NewCommand(ctx context.Context, reporter Reporter, resolve options.Resolve, stdout io.Writer) *cobra.Command {
	asJSON := false

	command := &cobra.Command{
		Use:   "status",
		Short: "Print the settings and the current snapshot",
		Long: `Print the resolved settings, the files clankerwatch reads, and the snapshot
the daemon would publish right now.

It reads Claude Code's files and the saved state only. It never calls the
usage endpoint, so it is safe to run at any time.`,
		Example: `# Show the settings and snapshot.
clankerwatch status

# Print the report as JSON.
clankerwatch status --json | jq .snapshot.bars`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := resolve()
			if err != nil {
				return err
			}

			report := reporter.Status(ctx, cfg)
			if asJSON {
				return writeJSON(stdout, report)
			}

			return writeText(stdout, report)
		},
	}
	command.Flags().BoolVar(&asJSON, flagJSON, false, "print the report as JSON")

	return command
}

// writeJSON writes the report with the snapshot embedded as an object.
//
// Parameters:
//   - stdout: the output stream.
//   - report: the report.
//
// Returns:
//   - error: an encode or write error.
func writeJSON(stdout io.Writer, report daemon.Report) error {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}

	err = options.WriteString(stdout, string(content)+"\n")
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	return nil
}

// writeText writes the report as aligned text followed by the snapshot.
//
// Parameters:
//   - stdout: the output stream.
//   - report: the report.
//
// Returns:
//   - error: a write error.
func writeText(stdout io.Writer, report daemon.Report) error {
	var table bytes.Buffer

	writer := tabwriter.NewWriter(&table, 0, 0, tabPadding, ' ', 0)
	rows := [][2]string{
		{"version", report.Version},
		{"mode", string(report.Mode)},
		{"intervals", fmt.Sprintf("%v active, %v idle", report.Interval, report.IdleInterval)},
		{"alerts", fmt.Sprintf("%v, login alerts %v", report.Thresholds, report.NotifyAuth)},
		{"credentials", report.Credentials},
		{"global config", report.GlobalConfig},
		{"sessions", report.Sessions},
		{"state file", report.StateFile},
		{"settings file", settingsRow(report.SettingsFile)},
	}

	for _, row := range rows {
		_, _ = fmt.Fprintf(writer, "%s\t%s\n", row[0], row[1])
	}

	err := writer.Flush()
	if err != nil {
		return fmt.Errorf("%w: %w", options.ErrWriteOutput, err)
	}

	pretty := string(report.Snapshot)

	var indented bytes.Buffer

	if json.Indent(&indented, report.Snapshot, "", "  ") == nil {
		pretty = indented.String()
	}

	text := table.String() + "\nsnapshot:\n" + strings.TrimSpace(pretty) + "\n"

	err = options.WriteString(stdout, text)
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	return nil
}

// settingsRow describes the settings file for the text report.
//
// Parameters:
//   - path: the settings file path, or empty.
//
// Returns:
//   - string: the path, or a note that no settings file applies.
func settingsRow(path string) string {
	if path == "" {
		return "none"
	}

	return path
}
