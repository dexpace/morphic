// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
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
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.ConflictingRedecl {
			out = append(out, diagnostic)
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
// fold to opposite outcomes. Each report lists where the declaration it kept
// is written and the member every model composing the pair decided, in order.
func TestConflictOnce_IsReportedOncePerPairOfDeclarations(t *testing.T) {
	t.Parallel()
	const (
		common  = "/components/schemas/Common/properties/"
		failure = "/components/schemas/Failure/properties/success"
	)
	type report struct {
		at, node, says, kept string
		decided              []string
	}
	member := func(model int, field string) string {
		return fmt.Sprintf("property/composed/components/schemas/M%d/allOf/properties/%s", model, field)
	}
	for name, tc := range map[string]struct {
		models []string
		want   []report
	}{
		"three models compose one pair": {
			models: []string{composing("Common", "Failure"), composing("Common", "Failure"), composing("Common", "Failure")},
			want: []report{{
				at: common + "success", kept: failure,
				says:    "over the one at " + common + "success, which stays declared on its model; the same holds at each of 3 models composing both",
				decided: []string{member(0, "success"), member(1, "success"), member(2, "success")},
			}},
		},
		"one model": {
			models: []string{composing("Common", "Failure")},
			want: []report{{
				at: common + "success", node: member(0, "success"), kept: failure,
				says:    "over the one at " + common + "success, which stays declared on its model",
				decided: []string{member(0, "success")},
			}},
		},
		"two pairs": {
			models: []string{composing("Common", "Failure"), composing("Common", "Other"), composing("Common", "Failure"), composing("Common", "Other")},
			want: []report{
				{
					at: common + "success", kept: failure,
					says:    "kept what the declaration at " + failure + " says over the one at " + common + "success, which stays declared on its model; the same holds at each of 2 models",
					decided: []string{member(0, "success"), member(2, "success")},
				},
				{
					at: common + "success", kept: "/components/schemas/Other/properties/success",
					says:    "kept what the declaration at /components/schemas/Other/properties/success says over the one at " + common + "success, which stays declared on its model; the same holds at each of 2 models",
					decided: []string{member(1, "success"), member(3, "success")},
				},
			},
		},
		"one pair over two members": {
			models: []string{composing("Common", "Mixed"), composing("Common", "Mixed")},
			want: []report{
				{
					at: common + "code", kept: "/components/schemas/Mixed/properties/code", says: "field \"code\" disagree",
					decided: []string{member(0, "code"), member(1, "code")},
				},
				{
					at: common + "success", kept: "/components/schemas/Mixed/properties/success", says: "field \"success\" disagree",
					decided: []string{member(0, "success"), member(1, "success")},
				},
			},
		},
		"one pair, opposite outcomes": {
			models: []string{composing("Common", "Failure"), composing("Failure", "Common")},
			want: []report{
				{
					at: common + "success", node: member(0, "success"), kept: failure,
					says: "kept what the declaration at " + failure + " says", decided: []string{member(0, "success")},
				},
				{
					at: failure, node: member(1, "success"), kept: common + "success",
					says: "kept what the declaration at " + common + "success says", decided: []string{member(1, "success")},
				},
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
		for i, report := range tc.want {
			assert.Equal(t, report.at, string(got[i].Provenance.Pointer), name)
			assert.Equal(t, report.node, got[i].Provenance.Node, name)
			assert.Contains(t, got[i].Message, report.says, name)
			kept, decided := concerns(t, got[i])
			assert.Equal(t, ir.Provenance{Pointer: jsontext.Pointer(report.kept)}, kept, name)
			assert.Equal(t, report.decided, decided, name)
		}
	}
}

// concerns reads what a conflict report concerns besides its place: where the
// declaration it kept is written, first, and then the members it decided,
// each named on no source.
func concerns(t *testing.T, diagnostic ir.Diagnostic) (kept ir.Provenance, decided []string) {
	t.Helper()
	require.NotEmpty(t, diagnostic.Related)
	require.Equal(t, ir.RelatedKept, diagnostic.Related[0].Role)
	for _, related := range diagnostic.Related[1:] {
		require.Equal(t, ir.RelatedDecided, related.Role)
		require.Equal(t, ir.Provenance{Source: ir.NoSource, Node: related.Provenance.Node}, related.Provenance)
		decided = append(decided, related.Provenance.Node)
	}
	return diagnostic.Related[0].Provenance, decided
}

// TestConflictOnce_AModelsOwnDeclarationsListTheMember pins what a conflict
// among declarations one model writes itself concerns: no entry brings one, so
// the report names no member beside its place, and it lists the member it
// decided and where the declaration it kept is written all the same.
func TestConflictOnce_AModelsOwnDeclarationsListTheMember(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpec(`
    M:
      allOf:
        - {type: object, properties: {p: {type: string}}}
        - {type: object, properties: {p: {type: integer}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	got := conflicts(diags)
	require.Len(t, got, 1)
	assert.Equal(t, ir.Provenance{Pointer: "/components/schemas/M/allOf/0/properties/p"}, got[0].Provenance)
	kept, decided := concerns(t, got[0])
	assert.Equal(t, ir.Provenance{Pointer: "/components/schemas/M/allOf/1/properties/p"}, kept)
	assert.Equal(t, []string{"property/openapi/components/schemas/M/allOf/0/properties/p"}, decided)
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
