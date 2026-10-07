// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

package options

import (
	"errors"
	"fmt"
	"io"
)

// ErrWriteOutput is returned when a command cannot write its output.
var ErrWriteOutput = errors.New("write output")

// WriteString writes text to writer.
//
// Parameters:
//   - writer: destination stream.
//   - text: exact bytes to write.
//
// Returns:
//   - error: wrapped write failure, or nil.
func WriteString(writer io.Writer, text string) error {
	_, err := io.WriteString(writer, text)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWriteOutput, err)
	}

	return nil
}
