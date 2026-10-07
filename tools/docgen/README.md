# docgen

CLI documentation generator for clankerwatch.

## Overview

docgen walks the Cobra command tree and writes Hugo Markdown pages with frontmatter. It is the single source of the
site's CLI reference in `docs/content/cli-reference/`.

The tree comes from `cmd.DocRoot`, which builds the same command tree the binary runs, without any process
dependencies. No command is executed, so docgen never touches the network, D-Bus, or your files, and the reference
cannot drift from the shipped command line.

## Directory structure

```text
tools/docgen/
    doc.go            Package documentation
    main.go           Entry point and flag parsing
    models.go         Data structures for template rendering
    extractor.go      Cobra command tree introspection
    generator.go      Orchestration: output directory, recursive generation
    renderer.go       text/template rendering with Markdown and YAML helpers
    templates/
        command.tmpl  Single command page
        index.tmpl    CLI reference index page
```

## Architecture

docgen has three parts:

- **DocExtractor** (`extractor.go`) walks the `cobra.Command` tree and builds a `commandDoc` per command, with its
  title, description, usage line, examples, own flags, inherited flags, and, for the root, the index data.
- **DocGenerator** (`generator.go`) creates the output directory, renders the index page, then renders each command
  as a directory holding an `_index.md`, recursing into command groups.
- **TemplateRenderer** (`renderer.go`) executes the embedded templates and writes each page only after it rendered
  completely.

Every page carries Hugo frontmatter (`title`, `description`, `type: docs`). Titles and descriptions are quoted, so a
colon in help text cannot break the YAML.

## What is documented

Only commands and flags a user can run: hidden and deprecated ones are skipped, and so are Cobra's `help` and
`completion` commands, which Cobra adds only when the program runs.

The root command's flags are the settings flags every command accepts. The index page lists them once, and each
command page repeats them under "Global options".

## Determinism

Cobra sorts commands by name and pflag visits flags in name order, and the templates hold no timestamps. The same
command tree always produces the same bytes. A test runs docgen twice and compares the output, and another compares it
with the committed reference.

## Usage

```bash
go run ./tools/docgen -out ./docs/content/cli-reference
```

or through the Taskfile:

```bash
task docs
```

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-out` | `./docs/content/cli-reference` | Output directory for the generated pages |

## Output

```text
docs/content/cli-reference/
    _index.md                  CLI reference index
    demo/_index.md
    notify-test/_index.md
    serve/_index.md
    status/_index.md
    version/_index.md
```

docgen overwrites pages but never deletes them. After removing or renaming a command, delete
`docs/content/cli-reference/` before regenerating.

## Inline generation

docgen can also run through `go generate`:

```go
//go:generate go run github.com/nicholas-fedor/clankerwatch/tools/docgen -out ../../docs/content/cli-reference
```

## Dependencies

- `github.com/spf13/cobra` for command tree introspection.
- `github.com/spf13/pflag` for flag metadata.

## Notes

- docgen is part of the main module and has no `go.mod` of its own.
- The templates are embedded, so the working directory only affects the relative `-out` path.
- Run `task docs` after any change to command help, examples, or flags, and commit the regenerated pages.
  `TestDocgenMatchesCommittedReference` fails until you do.
