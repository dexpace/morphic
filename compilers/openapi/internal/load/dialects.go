// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import "github.com/dexpace/morphic/compilers/openapi/internal/oas"

// dialects reads the minor a document is read at: the source's, and for one a
// reference reaches the one its declared version names, or the source's where
// the table covers none, as for a bare schema file, which declares no version,
// or a Swagger 2.0 one. It reads each minor's rows (Rows) from spec once, so
// the documents read at a minor share them; a map keyed by minor holds no
// more entries than the table has minors.
type dialects struct {
	spec oas.Spec
	// version and minor are the source's, which the load phase has admitted.
	version, minor string
	rows           map[string]Rows
}

// of returns the version a document declaring declared is held to, its minor,
// and what the table says at that minor.
func (d *dialects) of(declared string) (version, minor string, rows Rows) {
	version, minor = d.version, d.minor
	if m, ok := supportedMinor(declared); ok {
		version, minor = declared, m
	}
	if r, built := d.rows[minor]; built {
		return version, minor, r
	}
	if d.rows == nil {
		d.rows = map[string]Rows{}
	}
	d.rows[minor] = rowsAt(d.spec, version)
	return version, minor, d.rows[minor]
}

// own returns what the table says at the source's minor.
func (d *dialects) own() Rows {
	_, _, rows := d.of(d.version)
	return rows
}
