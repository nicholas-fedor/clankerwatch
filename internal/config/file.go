// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// File is the content of the YAML settings file.
//
// Every field is a pointer so an absent key keeps the value beneath it. An
// empty notify list is present and turns threshold alerts off.
type File struct {
	// Mode selects the data source: hybrid or cache-only.
	Mode *string `yaml:"mode"`

	// Interval is the polling interval while Claude Code is active.
	Interval *time.Duration `yaml:"interval"`

	// IdleInterval is the polling interval while Claude Code is idle.
	IdleInterval *time.Duration `yaml:"idleInterval"`

	// Notify lists the alert thresholds in percent.
	Notify *[]float64 `yaml:"notify"`

	// NotifyAuth enables the alert for a login that needs attention.
	NotifyAuth *bool `yaml:"notifyAuth"`

	// LogLevel is debug, info, warn, or error.
	LogLevel *string `yaml:"logLevel"`

	// ClaudeConfigDir is Claude Code's config directory.
	ClaudeConfigDir *string `yaml:"claudeConfigDir"`

	// StateDir is the directory for the daemon's state.
	StateDir *string `yaml:"stateDir"`
}

const (
	// maxFileSize bounds the settings file. Real files are a few hundred bytes.
	maxFileSize = 64 << 10
)

var (
	// ErrInvalidFile wraps a settings file that cannot be read or decoded.
	ErrInvalidFile = errors.New("invalid settings file")

	// ErrFileTooLarge indicates a settings file above maxFileSize.
	ErrFileTooLarge = errors.New("settings file is too large")

	// ErrMultipleDocuments indicates a settings file with more than one YAML
	// document.
	ErrMultipleDocuments = errors.New("settings file holds more than one document")

	// ErrNotUTF8 indicates a settings file that is not UTF-8 text.
	ErrNotUTF8 = errors.New("settings file is not UTF-8 text")

	// ErrNotMapping indicates a settings document that is not a mapping of
	// setting names.
	ErrNotMapping = errors.New("settings file is not a mapping of setting names")

	// ErrInvalidKey indicates a key that is not a plain setting name.
	ErrInvalidKey = errors.New("setting names must be plain text")

	// ErrUnsupportedYAML indicates YAML anchors, aliases, or merge keys.
	ErrUnsupportedYAML = errors.New("anchors, aliases, and merge keys are not supported")
)

// LoadFile reads and strictly decodes a settings file that must exist.
//
// Unknown keys and values of the wrong type are errors, so a typo never
// passes silently. An empty file holds no settings.
//
// Parameters:
//   - path: the file to read.
//
// Returns:
//   - File: the decoded settings.
//   - error: an error wrapping ErrInvalidFile.
func LoadFile(path string) (File, error) {
	var file File

	data, err := readFile(path)
	if err != nil {
		return file, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	file, err = DecodeFile(data)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}

	return file, nil
}

// LoadOptionalFile reads a settings file like LoadFile, but a missing file
// holds no settings.
//
// Parameters:
//   - path: the file to read.
//
// Returns:
//   - File: the decoded settings.
//   - error: an error wrapping ErrInvalidFile.
func LoadOptionalFile(path string) (File, error) {
	file, err := LoadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}

	return file, err
}

// DecodeFile strictly decodes settings file content.
//
// Content without a document, such as a file of comments, holds no settings,
// and so does a document holding only a null. Otherwise the document must be
// a mapping of setting names. A second document, YAML anchors, aliases, merge
// keys, and content that is not UTF-8 are errors.
//
// Parameters:
//   - data: the YAML content.
//
// Returns:
//   - File: the decoded settings.
//   - error: an error wrapping ErrInvalidFile.
func DecodeFile(data []byte) (File, error) {
	var file File

	doc, err := parseDocument(data)
	if err != nil || doc == nil || isNull(doc.Content[0]) {
		return file, err
	}

	err = doc.Content[0].Load(&file, yaml.WithKnownFields())
	if err != nil {
		return File{}, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	return file, nil
}

// parseDocument parses settings content and checks its shape.
//
// Parameters:
//   - data: the YAML content.
//
// Returns:
//   - *yaml.Node: the document node, or nil when there is no document.
//   - error: an error wrapping ErrInvalidFile.
func parseDocument(data []byte) (*yaml.Node, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, ErrNotUTF8)
	}

	loader, err := yaml.NewLoader(bytes.NewReader(data), yaml.WithUniqueKeys(true))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	var doc yaml.Node

	err = loader.Load(&doc)
	if errors.Is(err, io.EOF) {
		return nil, nil //nolint:nilnil // Content without a document is valid and empty.
	}

	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	err = rejectMoreDocuments(loader)
	if err != nil {
		return nil, err
	}

	root := doc.Content[0]
	if isNull(root) {
		return &doc, nil
	}

	err = checkRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}

	return &doc, nil
}

// checkRoot accepts a mapping whose keys are plain setting names and whose
// values use no anchors, aliases, or merge keys.
//
// Parameters:
//   - root: the document's root node.
//
// Returns:
//   - error: ErrNotMapping, ErrInvalidKey, or ErrUnsupportedYAML.
func checkRoot(root *yaml.Node) error {
	if root.Kind != yaml.MappingNode {
		return ErrNotMapping
	}

	for index := 0; index < len(root.Content); index += 2 {
		key := root.Content[index]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" || key.Anchor != "" {
			return fmt.Errorf("%w at line %d", ErrInvalidKey, key.Line)
		}
	}

	return checkNodes(root)
}

// checkNodes rejects anchors, aliases, and merge keys anywhere below node.
//
// The settings file is edited by machine, and these constructs tie values
// together in ways a single-key edit would break.
//
// Parameters:
//   - node: the node to check.
//
// Returns:
//   - error: ErrUnsupportedYAML naming the line.
func checkNodes(node *yaml.Node) error {
	if node.Anchor != "" || node.Kind == yaml.AliasNode || node.Tag == "!!merge" {
		return fmt.Errorf("%w at line %d", ErrUnsupportedYAML, node.Line)
	}

	for _, child := range node.Content {
		err := checkNodes(child)
		if err != nil {
			return err
		}
	}

	return nil
}

// isNull reports whether a node is the null scalar, such as "~", with no anchor.
//
// Parameters:
//   - node: a parsed node.
//
// Returns:
//   - bool: true for an unanchored null.
func isNull(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Tag == "!!null" && node.Anchor == ""
}

// rejectMoreDocuments reads the rest of a stream and fails on any document
// with content. Empty documents, such as one left by a trailing "---", pass.
//
// Parameters:
//   - loader: the stream after its first document.
//
// Returns:
//   - error: an error wrapping ErrInvalidFile.
func rejectMoreDocuments(loader *yaml.Loader) error {
	for {
		var extra any

		err := loader.Load(&extra)

		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return fmt.Errorf("%w: %w", ErrInvalidFile, err)
		case extra != nil:
			return fmt.Errorf("%w: %w", ErrInvalidFile, ErrMultipleDocuments)
		default:
		}
	}
}

// Apply copies the present settings onto cfg.
//
// Parameters:
//   - cfg: the settings to update.
//
// Returns:
//   - error: an error naming the key whose value cannot be used.
func (f File) Apply(cfg *Config) error {
	if f.Mode != nil {
		mode, err := ParseMode(*f.Mode)
		if err != nil {
			return fmt.Errorf("mode: %w", err)
		}

		cfg.Mode = mode
	}

	if f.Notify != nil {
		thresholds, err := NormalizeThresholds(*f.Notify)
		if err != nil {
			return fmt.Errorf("notify: %w", err)
		}

		cfg.Thresholds = thresholds
	}

	if f.LogLevel != nil {
		level, err := ParseLogLevel(*f.LogLevel)
		if err != nil {
			return fmt.Errorf("logLevel: %w", err)
		}

		cfg.LogLevel = level
	}

	f.applyPlain(cfg)

	return nil
}

// applyPlain copies the settings that need no parsing.
//
// Parameters:
//   - cfg: the settings to update.
func (f File) applyPlain(cfg *Config) {
	if f.Interval != nil {
		cfg.Interval = *f.Interval
	}

	if f.IdleInterval != nil {
		cfg.IdleInterval = *f.IdleInterval
	}

	if f.NotifyAuth != nil {
		cfg.NotifyAuth = *f.NotifyAuth
	}

	if f.ClaudeConfigDir != nil {
		cfg.ClaudeDir = *f.ClaudeConfigDir
	}

	if f.StateDir != nil && *f.StateDir != "" {
		cfg.StateDir = *f.StateDir
	}
}

// readFile reads at most maxFileSize bytes of a file.
//
// Parameters:
//   - path: the file to read.
//
// Returns:
//   - []byte: the content.
//   - error: the open or read error, or ErrFileTooLarge.
func readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	if len(data) > maxFileSize {
		return nil, fmt.Errorf("%w: over %d bytes", ErrFileTooLarge, maxFileSize)
	}

	return data, nil
}
