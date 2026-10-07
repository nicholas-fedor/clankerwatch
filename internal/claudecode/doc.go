// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package claudecode reads the files Claude Code keeps on disk.
//
// Every reader is read-only, and nothing in this package writes to Claude
// Code's files. The credentials reader decodes only the fields the daemon
// needs and never the refresh credential, because refresh tokens are single
// use and refreshing one would sign Claude Code out. The access token is held
// in a Secret, which redacts itself in logs, formatted output, and JSON.
//
// Readers stat their file on every call and re-read it only after it changes.
// Claude Code saves files by renaming a temporary file over them, so the inode
// is part of the change check.
package claudecode
