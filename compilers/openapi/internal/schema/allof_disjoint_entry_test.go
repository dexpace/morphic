// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestAllOf_AnEntryLoweringAsItsValueSetIsReadAsBesideTheAllOf holds the
// report of an allOf whose inline entry writes a value set its keywords leave
// no kind, which the position lowers as: the entry's type, its set and each
// union it writes are each a conjunct, as beside the allOf, so a set saying
// where it is written that it holds no value draws no report, while an entry
// the position does not lower as is named "no value".
func TestAllOf_AnEntryLoweringAsItsValueSetIsReadAsBesideTheAllOf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, s, want string
	}{
		{"a const outside its type", `{allOf: [` + refString + `, {type: integer, const: z}]}`,
			"allOf/0: string, allOf/1/type: integer, allOf/1/const: string)"},
		{"a const beside a union of another kind",
			`{allOf: [{$ref: '#/components/schemas/U'}, {const: 1, oneOf: [{type: string}, {type: boolean}]}]}`,
			"allOf/1/const: integer, allOf/1/oneOf: string or boolean)"},
		{"a const and an enum of two kinds", `{allOf: [{$ref: '#/components/schemas/U'}, {const: 1, enum: [a]}]}`, ""},
		{"an empty enum, which nothing elects", `{allOf: [` + refString + `, {enum: []}]}`, "allOf/1: no value)"},
	} {
		doc, diags := parseFull(t, disjointSpec(tc.s, ""))
		require.NotNil(t, doc, tc.name)
		reports := disjointReports(diags)
		if tc.want == "" {
			assert.Empty(t, reports, tc.name)
			continue
		}
		require.Len(t, reports, 1, "%s: %+v", tc.name, diags)
		assert.Contains(t, reports[0].Message, tc.want, tc.name)
		assert.Equal(t, ir.SeverityWarning, reports[0].Severity, tc.name)
	}
}
