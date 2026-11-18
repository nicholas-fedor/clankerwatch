# Contributing to clankerwatch

Thank you for your interest in contributing to clankerwatch.

This guide explains how to report a problem and how the project fits together. It also covers building, testing, and preparing a change for review.

clankerwatch reads Claude Code's login to ask for your usage. It is responsible for a credential it did not issue. The [project rules](#project-rules) keep that use minimal and safe. They apply to every change, including small ones.

## Reporting a problem

Open an issue on [GitHub](https://github.com/nicholas-fedor/clankerwatch/issues). A useful report includes:

- The output of `clankerwatch version`.
- Your distribution and the output of `plasmashell --version`.
- What you expected and what happened instead.
- The output of `clankerwatch status`.
- The daemon's log from around the time of the problem. `journalctl --user -u clankerwatch -n 50` prints it.

[Troubleshooting](https://clankerwatch.nickfedor.com/troubleshooting/) explains how to turn on debug logging. The token never appears in the log at any level. The log and the status output do show your file paths. Review them before you share them.

### Security issues

Don't report a vulnerability in a public issue. Report it privately through [GitHub's security advisories](https://github.com/nicholas-fedor/clankerwatch/security/advisories/new) instead.

## Before you start a change

Open an issue before a large change, a new dependency, or a change to how clankerwatch reads Claude Code's files or calls the usage endpoint. Agreeing on the approach first saves rework on both sides. A typo fix or a small bug fix can go straight to a pull request.

## Setting up

### Prerequisites

| Tool | Needed for |
| --- | --- |
| [Go](https://go.dev/dl/) 1.27.4 or later | Building and testing |
| [Task](https://taskfile.dev/installation/) 3.x | Running the tasks |
| [golangci-lint](https://golangci-lint.run/welcome/install/) v2 | Linting and formatting |
| [betteralign](https://github.com/dkorunic/betteralign) | Struct alignment |
| [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) | Vulnerability checks |
| [mockery](https://vektra.github.io/mockery/) v3 | Regenerating mocks after an interface changes |
| Qt 6 `qmllint` and `qmltestrunner` in `/usr/lib/qt6/bin` | Linting and testing the widget's QML. Arch packages them in `qt6-declarative` |
| KDE Plasma 6.4 or later with a systemd user session | Installing and running the widget |
| `plasmoidviewer` from Plasma's SDK | Optional. Viewing the widget outside a panel |
| [Hugo](https://gohugo.io/installation/) 0.146.0 or later | Previewing the documentation site |
| [GoReleaser](https://goreleaser.com/install/) | Optional. Building the release packages locally |

### First build

```bash
git clone https://github.com/nicholas-fedor/clankerwatch.git
cd clankerwatch
task check
```

`task check` runs everything a change must pass. A fresh clone passes it. If yours fails, compare your tool versions with the table above.

## Making a change

### Workflow

1. Make your change.
2. Run `task check` before every commit.
3. Commit any files it changed along with your change.

`task check` runs these tasks in order:

| Task | What it does |
| --- | --- |
| `task fmt` | Formats the code and aligns struct fields |
| `task vet` | Runs `go vet` |
| `task modernize` | Fails when `go fix` would change the code |
| `task test` | Runs every Go test with the race detector and requires at least 80% coverage |
| `task lint` | Runs golangci-lint and the struct alignment check |
| `task qmllint` | Lints the widget's QML |
| `task qmltest` | Runs the widget's QML tests |
| `task policy-check` | Fails on token refresh code and on a bus name that differs between files |
| `task vuln` | Reports known vulnerabilities in the dependency graph |

`task modernize FIX=true` applies the `go fix` changes. Run `task` alone to list every task.

Pull request checks run the Go tests, the linters, `task modernize`, `task policy-check`, and govulncheck. They don't run the QML checks, which need Qt 6 and Plasma. Run `task check` locally for every change to the widget.

### How it fits together

clankerwatch has two halves that talk over the D-Bus session bus. The daemon owns all logic and publishes one JSON snapshot. The widget only renders that snapshot. [How it works](https://clankerwatch.nickfedor.com/how-it-works/) describes the architecture and the D-Bus contract in full.

| Path | Holds |
| --- | --- |
| `main.go` | The entry point, which runs `internal/app` |
| `internal/app` | The composition root that wires dependencies together and maps errors to exit codes |
| `internal/cmd` | The command tree, with one package per command and `options` for the shared flags |
| `internal/claudecode` | Read-only access to the files Claude Code keeps on disk |
| `internal/usage` | The usage model and the client for the usage endpoint |
| `internal/schedule` | When the daemon calls the usage endpoint |
| `internal/engine` | The loop that fetches usage and builds the snapshot |
| `internal/notify` | Alert thresholds and desktop notifications |
| `internal/bus` | The D-Bus service that publishes the snapshot |
| `internal/daemon` | The operations each command runs, wired from the packages above |
| `internal/config`, `internal/state` | The settings with their validation, and the state kept across restarts |
| `plasmoid/` | The Plasma widget, written in QML only |
| `build/` | Tool configs, plus the systemd unit, D-Bus activation file, and example settings under `build/package/` |
| `test/qml/` | The widget's QML tests |
| `tools/docgen` | The generator for the CLI reference |
| `docs/` | The documentation site |

The `Snapshot` and `Settings` JSON documents are the contract between the daemon and the widget. A change to either one updates the daemon, the widget, and the D-Bus section of How it works in the same pull request. Both documents carry `"v": 1`. An incompatible change raises it.

### Project rules

These rules take priority over every other goal:

- **clankerwatch never writes Claude Code's files.** It opens them for reading only. The daemon's own files live in its own directories.
- **It never refreshes the token.** Claude Code's refresh tokens rotate and are single use. A refresh by any other program signs Claude Code out. `task policy-check` fails on token refresh code.
- **It reads only what it needs.** The credentials decoder skips the refresh token. The daemon never opens Claude Code's `.key` files.
- **The token goes to one place.** It is sent only to `https://api.anthropic.com/api/oauth/usage`. The client refuses redirects. The daemon makes no other network connection and sends no telemetry.
- **The token never leaks.** It lives in a type that prints as a redaction marker in every format. It is never logged or stored.
- **Every request identifies itself honestly** as `clankerwatch/<version>`. clankerwatch never poses as Claude Code.
- **Requests stay rare.** The daemon asks at most every 2 minutes, and every 5 minutes by default. It uses Claude Code's own cached result whenever that is newer, and it honors the endpoint's backoff.
- **The widget only renders.** It starts no processes and never polls. Its one timer updates countdown text.
- **The bus name is the same everywhere.** `task policy-check` compares `com.nickfedor.ClankerWatch1` across the files that name it.

[Security](https://clankerwatch.nickfedor.com/security/) explains each rule for users.

### Dependencies

clankerwatch prefers the standard library and a little code of its own over a new module. Propose a new module in an issue first. Say who maintains it and which modules it pulls in. `go mod graph` and `go mod why -m` show them.

## Working on the widget

Plasma loads a widget's QML only when it starts, so the widget needs a reinstall and a restart to show a change.

- `task update` reinstalls from your checkout and restarts the daemon. It restarts plasmashell only when the widget's files changed, which briefly blinks the panel.
- `task demo` stops the service and replays canned states about every 12 seconds. It covers every state the widget shows, without reading Claude Code's files or using the network. `systemctl --user start clankerwatch` returns to real data afterwards.
- `plasmoidviewer -a plasmoid` shows the widget from your checkout in its own window. Run it with `task demo` to see each state without touching your panel.

`task uninstall` removes the user install and keeps your settings and state.

## Tests

Put each test in the lowest tier that can prove the behavior.

| Tier | Where | Run by |
| --- | --- | --- |
| Unit | `<file>_test.go` beside the code, in the same package | `task test` |
| Integration | `<package>_integration_test.go`, in the `<package>_test` package | `task test` |
| Fuzz | `<package>_fuzz_test.go`, with regression seeds under `testdata/fuzz/` | `task fuzz` |
| Benchmark | `<package>_benchmark_test.go`, for hot paths only | `task bench` |
| QML | `test/qml/tst_*.qml` | `task qmltest` |

- Use [testify](https://github.com/stretchr/testify). Call `t.Parallel()` unless the test touches package globals or uses `t.Setenv`.
- Test time-driven code with `testing/synctest`.
- No test reads your real home directory or Claude Code's real files. Use `t.TempDir()`, and point `HOME` and the XDG variables at a temporary directory in a package that resolves real paths.
- No test makes an external call. Use an `httptest` server for the usage endpoint and a private `dbus-daemon` for the session bus.
- Give every decoder of outside input a fuzz target, such as a reader of Claude Code's files. `task fuzz` runs each one for 30 seconds. `task fuzz FUZZTIME=5m` runs them longer.

`go test ./internal/engine` runs one package's tests. Add `-run TestName` to run a single test.

## Generated files

Never edit a generated file by hand. Change its source instead.

| Generated file | Source | How it is updated |
| --- | --- | --- |
| `**/mocks/*.go` | The interfaces they mock | Run `task mocks` and commit the result |
| `docs/content/cli-reference/` | The command tree and its help text | A workflow opens a pull request after a change lands on `main`. `task docs` previews it |
| `CHANGELOG.md` | The commit messages | A workflow opens a pull request after a change lands on `main` |

## Code style

`task fmt` handles formatting. This section covers comments and prose.

### Doc comments

- Every Go file starts with the copyright line and `SPDX-License-Identifier: AGPL-3.0-or-later`.
- Every package has a `doc.go`.
- Every declaration has a doc comment. It is a complete sentence that starts with the name.
- A function's comment then lists `Parameters:` and `Returns:`. It omits an empty section.
- Every test has a doc comment that says what it covers.
- Every `//nolint` names the linter and gives the reason.

```go
// Fetch requests the usage payload.
//
// Parameters:
//   - ctx: cancellation and deadline for the request.
//   - bearer: the OAuth access token.
//
// Returns:
//   - []byte: the payload on HTTP 200.
//   - error: a *FetchError for every failure.
func (c *Client) Fetch(ctx context.Context, bearer string) ([]byte, error) {
```

Comments describe what the code does now and why. History belongs in commit messages.

### Writing

These rules apply to comments, documentation, help text, and the widget's text:

- Write short sentences that each say one thing.
- Don't chain three or more clauses with commas. Split them or use a list.
- Don't use em dashes. Don't join clauses with semicolons.

## Documentation

The documentation site lives in `docs/`. It's a [Hugo](https://gohugo.io/) site with the [Hextra](https://imfing.github.io/hextra/) theme. Preview it with live reload at `http://localhost:1313`:

```bash
task docs:serve
```

When you change behavior or settings, update the matching page in the same pull request. Follow these rules for documentation:

- Keep the README to an introduction and links. Details belong on the site.
- Never hard-wrap Markdown. Each paragraph and each list item is one line.
- Build pages to scan. Use tables, steps, and short lists instead of long paragraphs.

## Commits and pull requests

### Commit messages

clankerwatch uses [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). [git-cliff](https://git-cliff.org/) builds the changelog from commit messages. A clear message therefore becomes a clear release note.

```text
fix(plasmoid): show the limits the account has in the panel

- Show the first two non-credit limits instead of fixed session and weekly rows
- Add a demo scene for accounts without a session limit
```

- The type is one of `feat`, `fix`, `perf`, `refactor`, `style`, `docs`, `test`, `build`, `ci`, `chore`, or `revert`.
- The optional scope names the area of the change. `engine` and `plasmoid` are examples.
- The subject is imperative and lowercase with no trailing period.
- The body is a short list of the changes. It leaves out what the diff already shows.

### Pull requests

Keep each pull request to one change. Before you open it, check that:

- `task check` passes.
- New behavior has tests in the lowest tier that proves it.
- Mocks are regenerated and committed.
- The documentation site describes any change to behavior or settings.

In the description, say what problem the change solves and how. Link the issue it closes.

## Releases

A maintainer tags a release from `main` with `task tag:bump-patch`, `task tag:bump-minor`, or `task tag:bump-major`. The task fetches `origin` first. It refuses unless `HEAD` is a clean `main` that matches `origin/main` and has no release tag. It asks for confirmation and then pushes an annotated `vX.Y.Z` tag. The tag starts the release workflow, which publishes the packages with a signed checksum file.

`task release:snapshot` builds every package locally without signing or publishing anything.

## License

clankerwatch is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE.md). Contributions are accepted under the same license.
