// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import "github.com/dexpace/morphic/compilers/openapi/internal/oas"

// dialects reads the minor a document a reference reaches is read at: the one
// its declared version names, or the source's where the table covers none, as
// for a bare schema file, which declares no version, or a Swagger 2.0 one. It
// reads each minor's Schema Object rows from spec once, so the documents
// declaring a minor share them; a map keyed by minor holds no more entries
// than the table has minors.
type dialects struct {
	spec oas.Spec
	// version and minor are the source's, which the load phase has admitted.
	version, minor string
	rules          map[string]oas.SchemaRules
}

// of returns the version a document declaring declared is held to, its minor,
// and the Schema Object's rows at that minor.
func (d *dialects) of(declared string) (version, minor string, rules oas.SchemaRules) {
	version, minor = d.version, d.minor
	if m, ok := SupportedMinor(declared); ok {
		version, minor = declared, m
	}
	if r, built := d.rules[minor]; built {
		return version, minor, r
	}
	if d.rules == nil {
		d.rules = map[string]oas.SchemaRules{}
	}
	d.rules[minor] = d.spec.SchemaRules(version)
	return version, minor, d.rules[minor]
}
