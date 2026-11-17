// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// conflicts returns the conflicting-redeclaration reports of diags.
func conflicts(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.ConflictingRedecl {
			out = append(out, d)
		}
	}
	return out
}

// onceParts declares Common and Failure, whose success disagrees, Other, whose
// success disagrees with Common's too, and Mixed, whose success and code both
// disagree with Common's.
const onceParts = `    Common:
      type: object
      properties:
        success: {type: boolean, enum: [true]}
        code: {type: integer}
    Failure:
      type: object
      properties:
        success: {type: boolean, enum: [false]}
    Other:
      type: object
      properties:
        success: {type: string}
    Mixed:
      type: object
      properties:
        success: {type: boolean, enum: [false]}
        code: {type: string}
`

// composing is a schema composing the components named, in order.
func composing(names ...string) string {
	entries := make([]string, len(names))
	for i, name := range names {
		entries[i] = "{$ref: '#/components/schemas/" + name + "'}"
	}
	return "{allOf: [" + strings.Join(entries, ", ") + "]}"
}

// TestConflictOnce_IsReportedOncePerPairOfDeclarations pins how often a
// conflict between two declarations is reported (GitHub #2059): once, however
// many models compose both, where the declaration that lost is written,
// saying how many models compose them and naming no member, or naming the
// member where one model does. Each distinct conflict stays: two pairs, one
// pair over two members, one member between two pairs, and one pair two models
// fold to opposite outcomes.
func TestConflictOnce_IsReportedOncePerPairOfDeclarations(t *testing.T) {
	t.Parallel()
	const (
		common  = "/components/schemas/Common/properties/"
		failure = "/components/schemas/Failure/properties/success"
	)
	type report struct{ at, node, says string }
	for name, tc := range map[string]struct {
		models []string
		want   []report
	}{
		"three models compose one pair": {
			models: []string{composing("Common", "Failure"), composing("Common", "Failure"), composing("Common", "Failure")},
			want:   []report{{common + "success", "", "over the one at " + common + "success, which stays declared on its model; the same holds at each of 3 models composing both"}},
		},
		"one model": {
			models: []string{composing("Common", "Failure")},
			want:   []report{{common + "success", "p/composed/components/schemas/M0/allOf/properties/success", "over the one at " + common + "success, which stays declared on its model"}},
		},
		"two pairs": {
			models: []string{composing("Common", "Failure"), composing("Common", "Other"), composing("Common", "Failure"), composing("Common", "Other")},
			want: []report{
				{common + "success", "", "kept what the declaration at " + failure + " says over the one at " + common + "success, which stays declared on its model; the same holds at each of 2 models"},
				{common + "success", "", "kept what the declaration at /components/schemas/Other/properties/success says over the one at " + common + "success, which stays declared on its model; the same holds at each of 2 models"},
			},
		},
		"one pair over two members": {
			models: []string{composing("Common", "Mixed"), composing("Common", "Mixed")},
			want: []report{
				{common + "code", "", "field \"code\" disagree"},
				{common + "success", "", "field \"success\" disagree"},
			},
		},
		"one pair, opposite outcomes": {
			models: []string{composing("Common", "Failure"), composing("Failure", "Common")},
			want: []report{
				{common + "success", "p/composed/components/schemas/M0/allOf/properties/success", "kept what the declaration at " + failure + " says"},
				{failure, "p/composed/components/schemas/M1/allOf/properties/success", "kept what the declaration at " + common + "success says"},
			},
		},
	} {
		schemas := onceParts
		for i, m := range tc.models {
			schemas += fmt.Sprintf("    M%d: %s\n", i, m)
		}
		_, diags := parseFull(t, openapitest.ComponentSpec(schemas))
		openapitest.RequireNoErrorDiags(t, diags)

		got := conflicts(diags)
		require.Len(t, got, len(tc.want), "%s: %+v", name, got)
		for i, w := range tc.want {
			assert.Equal(t, w.at, string(got[i].Provenance.Pointer), name)
			assert.Equal(t, w.node, got[i].Provenance.Node, name)
			assert.Contains(t, got[i].Message, w.says, name)
		}
	}
}

// TestConflictOnce_InlineCompositionsAtOperationsAreOneReport pins the shape
// the issue measured: each operation's response composes the same two
// components inline, so each lowers its own composition, and the conflict
// between them is reported once, whichever order the operations and the
// components are written in.
func TestConflictOnce_InlineCompositionsAtOperationsAreOneReport(t *testing.T) {
	t.Parallel()
	operation := func(path string) string {
		return "  /" + path + ":\n    get:\n      operationId: " + path + "\n      responses:\n        '400':\n" +
			"          description: failed\n          content:\n            application/json:\n" +
			"              schema: " + composing("Common", "Failure") + "\n"
	}
	components := []string{
		"    Common: {type: object, properties: {success: {type: boolean, enum: [true]}}}\n",
		"    Failure: {type: object, properties: {success: {type: boolean, enum: [false]}}}\n",
	}
	spec := func(paths, schemas []string) string {
		return openapitest.PathsSpec(strings.Join(paths, "") + "components:\n  schemas:\n" + strings.Join(schemas, ""))
	}
	paths := []string{operation("a"), operation("b"), operation("c")}
	_, diags := parseFull(t, spec(paths, components))
	openapitest.RequireNoErrorDiags(t, diags)

	got := conflicts(diags)
	require.Len(t, got, 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/Common/properties/success", string(got[0].Provenance.Pointer))
	assert.Empty(t, got[0].Provenance.Node)
	assert.True(t, strings.HasSuffix(got[0].Message, "; the same holds at each of 3 models composing both"), got[0].Message)

	for name, swapped := range map[string]string{
		"the components swapped": spec(paths, []string{components[1], components[0]}),
		"the operations swapped": spec([]string{paths[2], paths[0], paths[1]}, components),
	} {
		_, again := parseFull(t, swapped)
		assert.Empty(t, cmp.Diff(diags, again), name)
	}
}
