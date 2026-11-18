// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

// passedOverRefs are $ref uses beside a value set the election of competing
// keywords elects over the $ref: a const and an enum sharing no value beside a
// string, and an enum beside a reference naming no schema.
var passedOverRefs = map[string]struct{ target, keywords string }{
	"disjoint value keywords":    {"T: {type: string}", "const: z, enum: [a]"},
	"a reference naming nothing": {"Other: {type: string}", "enum: [a]"},
}

// TestRefConjunction_APassedOverRefIsKeptUnderItsKeyInEverySpelling holds a
// $ref the election of competing keywords passes over, written beside the
// value set or as an allOf of it beside the value set, to one document at
// each site: the reference kept under openapi:$ref holding its text, the
// allOf of it no more kept whole, up to the entry's pointer.
func TestRefConjunction_APassedOverRefIsKeptUnderItsKeyInEverySpelling(t *testing.T) {
	t.Parallel()
	ref := "$ref: '#/components/schemas/T'"
	for _, site := range conjSites {
		for name, tc := range passedOverRefs {
			beside := writtenAnyway(t, site, tc.target, "{"+ref+", "+tc.keywords+"}")
			wrapped := writtenAnyway(t, site, tc.target, "{allOf: [{"+ref+"}], "+tc.keywords+"}")
			assert.Empty(t, cmp.Diff(beside, wrapped, cmp.AllowUnexported(entryWritten{})), "%s, %s", site.name, name)
			assert.Contains(t, wrapped.doc, `"openapi:$ref":{"reason":"degraded_lowering","value":"#/components/schemas/T"`,
				"%s, %s", site.name, name)
		}
	}
}
