// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules

import (
	"maps"
	"slices"
	"strings"
)

// Plural is plural, for the external tests.
var Plural = plural

// TemplateFault is templateFault, for the external tests.
var TemplateFault = templateFault

// FragmentAt is fragmentAt, for the external tests.
var FragmentAt = fragmentAt

// Objects returns the table's objects in declaration order, for the tests
// that hold every row to the table's own rules. The engine asks the table by
// kind alone.
func (s Spec) Objects() []Object { return s.objects }

// SchemaDialectIDs returns the id of each dialect the table has rows for, for
// the external tests reading a schema with every row.
func SchemaDialectIDs() []string {
	dialects := schemaDialects()
	ids := make([]string, 0, len(dialects))
	for _, d := range dialects {
		ids = append(ids, d.id)
	}
	return ids
}

// KeywordsOf returns the fields r defines and those it defines later, each
// sorted by name, for the external tests reading every keyword of a row.
func KeywordsOf(r SchemaRules) (defined, later []Field) {
	byName := func(a, b Field) int { return strings.Compare(a.Name, b.Name) }
	return slices.SortedFunc(maps.Values(r.rows.fields), byName), slices.SortedFunc(maps.Values(r.rows.later), byName)
}
