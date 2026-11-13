// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report

import (
	"encoding/json/v2"
	"fmt"
	"io"
)

// SchemaVersion numbers the shape of the report WriteJSON writes. It moves
// with any change to that shape, a field of a finding's diagnostic included,
// so a consumer reading the version knows every member it will meet.
const SchemaVersion = 1

// versioned is the report WriteJSON encodes: r's members after the version.
type versioned struct {
	Version int `json:"version"`
	Report  `json:",embed"`
}

// WriteJSON writes r to w as one JSON object, a newline after it, for a
// program to read: its schema version, the verdict, the sources, every
// finding as rated, each with the diagnostic as reported and the rule it met,
// the unused rules and the warnings, each list present when empty. It holds
// every string exactly, escaped as JSON escapes it, and writes members in a
// fixed order, so one run's report is one string of bytes.
//
// It refuses a nil w, and a report holding a string that is not UTF-8, which
// JSON cannot carry, before writing anything.
func WriteJSON(w io.Writer, r Report) error {
	if w == nil {
		return errNoWriter
	}
	data, err := json.Marshal(versioned{Version: SchemaVersion, Report: r}, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("report: encode: %w", err)
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
