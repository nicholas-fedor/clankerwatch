// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// entry is one key of a settings patch and its YAML value.
type entry struct {
	value any
	key   string
}

// span is the lines one top-level key occupies in a settings file.
type span struct {
	// comment is the key's or value's trailing comment, kept on the new line.
	comment string

	// start is the key's line index.
	start int

	// end is the index of the first line after the value.
	end int
}

// Settings file keys.
const (
	// KeyMode is the data source key.
	KeyMode = "mode"

	// KeyInterval is the active polling interval key.
	KeyInterval = "interval"

	// KeyIdleInterval is the idle polling interval key.
	KeyIdleInterval = "idleInterval"

	// KeyNotify is the alert thresholds key.
	KeyNotify = "notify"

	// KeyNotifyAuth is the login alert key.
	KeyNotifyAuth = "notifyAuth"

	// KeyLogLevel is the log level key.
	KeyLogLevel = "logLevel"

	// KeyClaudeConfigDir is Claude Code's config directory key.
	KeyClaudeConfigDir = "claudeConfigDir"

	// KeyStateDir is the state directory key.
	KeyStateDir = "stateDir"
)

const (
	// fileMode is the settings file's permission bits.
	fileMode = 0o600

	// dirMode is the permission bits of a settings directory the daemon creates.
	dirMode = 0o700

	// rootColumn is the column of a top-level key in a block mapping.
	rootColumn = 1
)

var (
	// ErrEditFailed indicates that no edit of the settings file produced the
	// requested settings.
	ErrEditFailed = errors.New("the settings file cannot be updated automatically")

	// errNotSpliceable indicates a layout the line editor does not handle.
	errNotSpliceable = errors.New("layout needs re-encoding")
)

// UpdateFile returns settings file content with the present keys of patch set.
//
// Edits keep comments, blank lines, and every key the patch leaves out. In
// content without a document, such as the commented example, a key replaces
// its commented-out line, so the documentation stays next to it. In a block
// mapping, a key's lines are replaced in place. Other layouts are re-encoded.
// Every result is decoded again and must hold exactly the previous settings
// with the patch applied.
//
// Parameters:
//   - data: the current content, which may be empty.
//   - patch: the settings to set.
//
// Returns:
//   - []byte: the updated content, or data itself for an empty patch.
//   - error: an error wrapping ErrInvalidFile for content that does not
//     decode, or ErrEditFailed.
func UpdateFile(data []byte, patch File) ([]byte, error) {
	before, err := DecodeFile(data)
	if err != nil {
		return nil, err
	}

	entries := patch.entries()
	if len(entries) == 0 {
		return data, nil
	}

	want := before.with(patch)

	doc, err := parseDocument(data)
	if err != nil {
		return nil, err
	}

	candidate, err := edit(data, doc, entries)
	if err == nil && holds(candidate, want) {
		return candidate, nil
	}

	candidate, err = reencode(doc, entries)
	if err != nil {
		return nil, err
	}

	if !holds(candidate, want) {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, ErrEditFailed)
	}

	return candidate, nil
}

// SaveFile writes settings file content atomically with mode 0600.
//
// The content goes to a temporary file in the same directory, which then
// replaces the file. A missing directory is created with mode 0700.
//
// Parameters:
//   - path: the settings file.
//   - data: the content.
//
// Returns:
//   - error: the first failing file operation.
func SaveFile(path string, data []byte) error {
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, dirMode)
	if err != nil {
		return fmt.Errorf("create the settings directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create a temporary settings file: %w", err)
	}

	defer func() { _ = os.Remove(temp.Name()) }()

	err = writeAndClose(temp, data)
	if err != nil {
		return err
	}

	err = os.Rename(temp.Name(), path)
	if err != nil {
		return fmt.Errorf("replace the settings file: %w", err)
	}

	return nil
}

// FormatDuration writes a duration the way people type it, such as 5m or 1h30m.
//
// Parameters:
//   - duration: the duration.
//
// Returns:
//   - string: the duration without zero minute or second units.
func FormatDuration(duration time.Duration) string {
	text := duration.String()

	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}

	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}

	return text
}

// entries lists the patch's present keys in file order.
//
// Returns:
//   - []entry: the keys to set and their values.
func (f File) entries() []entry {
	var entries []entry

	add := func(present bool, key string, value func() any) {
		if present {
			entries = append(entries, entry{key: key, value: value()})
		}
	}

	add(f.Mode != nil, KeyMode, func() any { return *f.Mode })
	add(f.Interval != nil, KeyInterval, func() any { return FormatDuration(*f.Interval) })
	add(f.IdleInterval != nil, KeyIdleInterval, func() any { return FormatDuration(*f.IdleInterval) })
	add(f.Notify != nil, KeyNotify, func() any { return *f.Notify })
	add(f.NotifyAuth != nil, KeyNotifyAuth, func() any { return *f.NotifyAuth })
	add(f.LogLevel != nil, KeyLogLevel, func() any { return *f.LogLevel })
	add(f.ClaudeConfigDir != nil, KeyClaudeConfigDir, func() any { return *f.ClaudeConfigDir })
	add(f.StateDir != nil, KeyStateDir, func() any { return *f.StateDir })

	return entries
}

// with returns the settings with the present keys of patch replacing them.
//
// Parameters:
//   - patch: the settings to set.
//
// Returns:
//   - File: the combined settings.
func (f File) with(patch File) File {
	out := f

	if patch.Mode != nil {
		out.Mode = patch.Mode
	}

	if patch.Interval != nil {
		out.Interval = patch.Interval
	}

	if patch.IdleInterval != nil {
		out.IdleInterval = patch.IdleInterval
	}

	if patch.Notify != nil {
		out.Notify = patch.Notify
	}

	return out.withPlain(patch)
}

// withPlain copies the patch's remaining keys, split from with to keep each
// function short.
//
// Parameters:
//   - patch: the settings to set.
//
// Returns:
//   - File: the combined settings.
func (f File) withPlain(patch File) File {
	if patch.NotifyAuth != nil {
		f.NotifyAuth = patch.NotifyAuth
	}

	if patch.LogLevel != nil {
		f.LogLevel = patch.LogLevel
	}

	if patch.ClaudeConfigDir != nil {
		f.ClaudeConfigDir = patch.ClaudeConfigDir
	}

	if patch.StateDir != nil {
		f.StateDir = patch.StateDir
	}

	return f
}

// edit applies entries while keeping the content's layout.
//
// Parameters:
//   - data: the current content.
//   - doc: the parsed document, or nil when there is none.
//   - entries: the keys to set.
//
// Returns:
//   - []byte: the edited content.
//   - error: errNotSpliceable for a layout that needs re-encoding.
func edit(data []byte, doc *yaml.Node, entries []entry) ([]byte, error) {
	switch {
	case doc == nil:
		return uncomment(data, entries)
	case isNull(doc.Content[0]):
		return nil, errNotSpliceable
	default:
		return splice(data, doc.Content[0], entries)
	}
}

// uncomment sets keys in content without a document.
//
// Each key replaces its commented-out line, such as "#interval: 5m". Keys
// without such a line are appended.
//
// Parameters:
//   - data: content holding only comments or nothing.
//   - entries: the keys to set.
//
// Returns:
//   - []byte: the updated content.
//   - error: the encoding error.
func uncomment(data []byte, entries []entry) ([]byte, error) {
	text := string(data)

	for _, item := range entries {
		line, err := renderLine(item)
		if err != nil {
			return nil, err
		}

		pattern := regexp.MustCompile(`(?m)^#[ \t]*` + regexp.QuoteMeta(item.key) + `:.*$`)
		if location := pattern.FindStringIndex(text); location != nil {
			text = text[:location[0]] + line + text[location[1]:]

			continue
		}

		text = appendLine(text, line)
	}

	return []byte(text), nil
}

// splice replaces the lines of each patched key in a block mapping and
// appends keys the mapping lacks.
//
// Parameters:
//   - data: the current content.
//   - root: the document's root mapping.
//   - entries: the keys to set.
//
// Returns:
//   - []byte: the edited content.
//   - error: errNotSpliceable for a flow mapping or indented keys, or an
//     encoding error.
func splice(data []byte, root *yaml.Node, entries []entry) ([]byte, error) {
	if root.Style&yaml.FlowStyle != 0 {
		return nil, errNotSpliceable
	}

	lines := strings.SplitAfter(string(data), "\n")

	spans, err := keySpans(lines, root)
	if err != nil {
		return nil, err
	}

	replaced := make(map[int]string, len(entries))
	removed := make(map[int]bool)
	appended := make([]string, 0, len(entries))

	for _, item := range entries {
		line, err := renderLine(item)
		if err != nil {
			return nil, err
		}

		place, found := spans[item.key]
		if !found {
			appended = append(appended, line)

			continue
		}

		if place.comment != "" {
			line += " " + place.comment
		}

		replaced[place.start] = line + "\n"

		for index := place.start + 1; index < place.end; index++ {
			removed[index] = true
		}
	}

	return []byte(joinLines(lines, replaced, removed, appended)), nil
}

// keySpans finds the lines each top-level key occupies.
//
// Parameters:
//   - lines: the content split after each newline.
//   - root: the document's root mapping.
//
// Returns:
//   - map[string]span: the lines per key.
//   - error: errNotSpliceable when a key is not at the start of its line.
func keySpans(lines []string, root *yaml.Node) (map[string]span, error) {
	spans := make(map[string]span, len(root.Content))

	for index := 0; index+1 < len(root.Content); index += 2 {
		key, value := root.Content[index], root.Content[index+1]
		if key.Column != rootColumn || key.Line < 1 || key.Line > len(lines) {
			return nil, errNotSpliceable
		}

		comment := value.LineComment
		if comment == "" {
			comment = key.LineComment
		}

		start := key.Line - 1

		spans[key.Value] = span{comment: comment, start: start, end: valueEnd(lines, start)}
	}

	return spans, nil
}

// valueEnd returns the index of the first line after a top-level key's value.
//
// A value continues on indented lines and on sequence items, which YAML
// allows at the key's own column. A blank line, a comment at the left
// margin, or the next key ends it.
//
// Parameters:
//   - lines: the content split after each newline.
//   - start: the key's line index.
//
// Returns:
//   - int: the first line index after the value.
func valueEnd(lines []string, start int) int {
	end := start + 1

	for end < len(lines) {
		line := strings.TrimRight(lines[end], "\r\n")

		continued := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") ||
			line == "-" || strings.HasPrefix(line, "- ")
		if !continued {
			break
		}

		end++
	}

	return end
}

// joinLines rebuilds content from its lines with replacements, removals, and
// appended lines.
//
// Parameters:
//   - lines: the content split after each newline.
//   - replaced: new text per line index.
//   - removed: line indexes to drop.
//   - appended: lines to add at the end, without newlines.
//
// Returns:
//   - string: the new content.
func joinLines(lines []string, replaced map[int]string, removed map[int]bool, appended []string) string {
	var builder strings.Builder

	for index, line := range lines {
		switch {
		case removed[index]:
		case replaced[index] != "":
			_, _ = builder.WriteString(replaced[index])
		default:
			_, _ = builder.WriteString(line)
		}
	}

	text := builder.String()

	for _, line := range appended {
		text = appendLine(text, line)
	}

	return text
}

// appendLine adds a line at the end of text, after a newline if needed.
//
// Parameters:
//   - text: the content.
//   - line: the line, without a newline.
//
// Returns:
//   - string: the content with the line.
func appendLine(text, line string) string {
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	return text + line + "\n"
}

// reencode applies entries through the YAML encoder.
//
// It handles every layout, including flow mappings and a null document, but
// can drop blank lines, so UpdateFile tries edit first.
//
// Parameters:
//   - doc: the parsed document, or nil when there is none.
//   - entries: the keys to set.
//
// Returns:
//   - []byte: the encoded content.
//   - error: the encoding error.
func reencode(doc *yaml.Node, entries []entry) ([]byte, error) {
	if doc == nil {
		doc = newNode(yaml.DocumentNode, "", "")
		doc.Content = []*yaml.Node{newNode(yaml.MappingNode, "!!map", "")}
	}

	root := doc.Content[0]
	if isNull(root) {
		mapping := newNode(yaml.MappingNode, "!!map", "")

		mapping.HeadComment = root.HeadComment
		mapping.LineComment = root.LineComment
		mapping.FootComment = root.FootComment
		doc.Content[0] = mapping
		root = mapping
	}

	for _, item := range entries {
		err := setKey(root, item)
		if err != nil {
			return nil, err
		}
	}

	out, err := yaml.Dump(doc, yaml.WithV4Defaults())
	if err != nil {
		return nil, fmt.Errorf("encode the settings file: %w", err)
	}

	return out, nil
}

// setKey sets one key in a mapping, replacing its value or appending it.
//
// A replaced value keeps its trailing comment.
//
// Parameters:
//   - mapping: a mapping node.
//   - item: the key and value.
//
// Returns:
//   - error: the encoding error.
func setKey(mapping *yaml.Node, item entry) error {
	value, err := valueNode(item.value)
	if err != nil {
		return err
	}

	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == item.key {
			value.LineComment = mapping.Content[index+1].LineComment
			mapping.Content[index+1] = value

			return nil
		}
	}

	mapping.Content = append(mapping.Content, newNode(yaml.ScalarNode, "!!str", item.key), value)

	return nil
}

// valueNode encodes a value, with lists in flow style such as [80, 95].
//
// Parameters:
//   - value: the value.
//
// Returns:
//   - *yaml.Node: the encoded value.
//   - error: the encoding error.
func valueNode(value any) (*yaml.Node, error) {
	node := newNode(0, "", "")

	err := node.Encode(value)
	if err != nil {
		return nil, fmt.Errorf("encode a setting: %w", err)
	}

	if node.Kind == yaml.SequenceNode {
		node.Style = yaml.FlowStyle
	}

	return node, nil
}

// renderLine renders one key as a single YAML line.
//
// Parameters:
//   - item: the key and value.
//
// Returns:
//   - string: a line such as "notify: [80, 95]", without a newline.
//   - error: the encoding error.
func renderLine(item entry) (string, error) {
	mapping := newNode(yaml.MappingNode, "!!map", "")

	err := setKey(mapping, item)
	if err != nil {
		return "", err
	}

	out, err := yaml.Dump(mapping, yaml.WithV4Defaults())
	if err != nil {
		return "", fmt.Errorf("encode a setting: %w", err)
	}

	return strings.TrimSuffix(string(out), "\n"), nil
}

// newNode returns an empty node of a kind.
//
// Parameters:
//   - kind: the node kind, or 0 for a node that Encode fills in.
//   - tag: the YAML tag.
//   - value: the scalar value.
//
// Returns:
//   - *yaml.Node: the node.
func newNode(kind yaml.Kind, tag, value string) *yaml.Node {
	return &yaml.Node{
		Kind:        kind,
		Style:       0,
		Tag:         tag,
		Value:       value,
		Anchor:      "",
		Alias:       nil,
		Content:     nil,
		HeadComment: "",
		LineComment: "",
		FootComment: "",
		Line:        0,
		Column:      0,
		Stream:      nil,
	}
}

// holds reports whether content decodes to exactly the wanted settings.
//
// Parameters:
//   - data: the content.
//   - want: the settings it must hold.
//
// Returns:
//   - bool: true when it decodes and matches.
func holds(data []byte, want File) bool {
	got, err := DecodeFile(data)

	return err == nil && reflect.DeepEqual(got, want)
}

// writeAndClose writes data to a temporary file, syncs it, and closes it.
//
// Parameters:
//   - file: the temporary file.
//   - data: the content.
//
// Returns:
//   - error: the first failing operation.
func writeAndClose(file *os.File, data []byte) error {
	_, err := file.Write(data)
	if err == nil {
		err = file.Chmod(fileMode)
	}

	if err == nil {
		err = file.Sync()
	}

	closeErr := file.Close()

	err = errors.Join(err, closeErr)
	if err != nil {
		return fmt.Errorf("write the settings file: %w", err)
	}

	return nil
}
