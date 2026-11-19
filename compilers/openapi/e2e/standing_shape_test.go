// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// shapeCase is a schema whose report names what its position lowered to: at,
// below the position, where the report is drawn, and flat, the same schema
// written with no allOf, "" for a $ref principal, whose position is a use of it.
type shapeCase struct{ name, schema, at, flat string }

// shapeCases write, beside an inline allOf principal of each kind, on its
// allOf or on another entry in either order, a keyword its node has no home
// for, and the same beside a $ref principal.
var shapeCases = []shapeCase{
	{"scalar beside", "{allOf: [{type: string, maxLength: 3}], minimum: 1}", "", "{type: string, maxLength: 3, minimum: 1}"},
	{"list beside", "{allOf: [{type: array, items: {type: string}}], minLength: 1}", "", "{type: array, items: {type: string}, minLength: 1}"},
	{"type list beside", "{allOf: [{type: [string, integer]}], minItems: 1}", "", "{type: [string, integer], minItems: 1}"},
	{"union beside", "{allOf: [{oneOf: [{type: array, items: {type: string}}, {type: array, items: {type: integer}}]}], minItems: 1}", "",
		"{oneOf: [{type: array, items: {type: string}}, {type: array, items: {type: integer}}], minItems: 1}"},
	{"collapsed union beside", "{allOf: [{oneOf: [{$ref: '#/components/schemas/L'}, {type: 'null'}]}], minLength: 1}", "",
		"{oneOf: [{$ref: '#/components/schemas/L'}, {type: 'null'}], minLength: 1}"},
	{"shared primitive beside", "{allOf: [{type: string}], minimum: 1}", "", "{type: string, minimum: 1}"},
	{"list on an entry", "{allOf: [{type: array, items: {type: string}}, {minLength: 1}]}", "/allOf/1",
		"{type: array, items: {type: string}, minLength: 1}"},
	{"list on an entry first", "{allOf: [{minLength: 1}, {type: array, items: {type: string}}]}", "/allOf/0",
		"{type: array, items: {type: string}, minLength: 1}"},
	{"value set on an entry", "{allOf: [{enum: [a, b]}, {minimum: 1}]}", "/allOf/1", "{enum: [a, b], minimum: 1}"},
	{"shared primitive on an entry", "{allOf: [{type: string}, {minimum: 1}]}", "/allOf/1", "{type: string, minimum: 1}"},
	{"$ref beside", "{allOf: [{$ref: '#/components/schemas/S'}], minimum: 1}", "", ""},
	{"$ref on an entry", "{allOf: [{$ref: '#/components/schemas/L'}, {minLength: 1}]}", "/allOf/1", ""},
}

// TestStandingShape_AReportNamesWhatAnInlinePrincipalLoweredTo pins the report
// of a keyword kept beside an inline allOf principal, at a component and a
// property, in both declaration orders: it names what the position lowered
// to as the same schema written flat does, and only a $ref principal's says
// the position lowered to a use of one of its conjuncts
// (docs/compilers/openapi.md#allof-refinement).
func TestStandingShape_AReportNamesWhatAnInlinePrincipalLoweredTo(t *testing.T) {
	t.Parallel()
	lines := []string{"    S: {type: string}", "    L: {type: array, items: {type: string}}"}
	for i, tc := range shapeCases {
		lines = append(lines, fmt.Sprintf("    C%d: %s", i, tc.schema),
			fmt.Sprintf("    M%d: {type: object, properties: {p: %s}}", i, tc.schema))
		if tc.flat != "" {
			lines = append(lines, fmt.Sprintf("    F%d: %s", i, tc.flat),
				fmt.Sprintf("    G%d: {type: object, properties: {p: %s}}", i, tc.flat))
		}
	}
	reversed := slices.Clone(lines)
	slices.Reverse(reversed)
	forward, backward := shapesReported(t, lines), shapesReported(t, reversed)
	for i, tc := range shapeCases {
		for _, pos := range []struct{ at, flat string }{
			{fmt.Sprintf("C%d", i), fmt.Sprintf("F%d", i)}, {fmt.Sprintf("M%d/properties/p", i), fmt.Sprintf("G%d/properties/p", i)},
		} {
			t.Run(tc.name+" "+pos.at, func(t *testing.T) {
				t.Parallel()
				got := forward[pos.at+tc.at]
				assert.Equal(t, got, backward[pos.at+tc.at], "the declaration order decides nothing")
				require.NotEmpty(t, got, "reported at %s", pos.at+tc.at)
				want := "a use of one of its allOf conjuncts"
				if tc.flat != "" {
					want = forward[pos.flat]
					require.NotEmpty(t, want, "the flat spelling reports at %s", pos.flat)
				}
				assert.Equal(t, want, got, "the report names what it lowered to as the flat spelling's does")
			})
		}
	}
}

// shapesReported compiles the schemas lines declares, in their order, and
// returns what each degraded-construct info saying a position has no home for
// a keyword names the position lowered to, by the pointer below
// /components/schemas it is drawn at.
func shapesReported(t *testing.T, lines []string) map[string]string {
	t.Helper()
	_, diags := parseFull(t, openapitest.ComponentSpec(strings.Join(lines, "\n")+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	out := map[string]string{}
	for _, d := range diags {
		_, rest, lowered := strings.Cut(d.Message, " lowered to ")
		shape, _, homed := strings.Cut(rest, ", which has no home")
		if d.Code == diag.DegradedConstruct && lowered && homed {
			out[strings.TrimPrefix(string(d.Provenance.Pointer), "/components/schemas/")] = shape
		}
	}
	return out
}

// TestStandingShape_ConjunctsSharingNoValueNameWhatThePositionLoweredTo pins
// the warning of a union beside an allOf sharing no kind with its principal:
// it names the node an inline principal stands as at the position, and a
// use of one of its conjuncts only for a $ref principal
// (docs/compilers/openapi.md#allof-disjoint-conjuncts).
func TestStandingShape_ConjunctsSharingNoValueNameWhatThePositionLoweredTo(t *testing.T) {
	t.Parallel()
	const union = "oneOf: [{type: integer}, {type: boolean}]"
	_, diags := parseFull(t, openapitest.ComponentSpec("    S: {type: string}\n"+
		"    L: {allOf: [{type: array, items: {type: string}}], "+union+"}\n"+
		"    R: {allOf: [{$ref: '#/components/schemas/S'}], "+union+"}\n"))
	want := map[string]string{
		"/components/schemas/L": `which a node of kind "list" cannot state`,
		"/components/schemas/R": "which a use of one of its allOf conjuncts cannot state",
	}
	got := map[string]string{}
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && strings.Contains(d.Message, "share no value") {
			_, tail, _ := strings.Cut(d.Message, "so no instance satisfies this schema, ")
			got[string(d.Provenance.Pointer)] = tail
		}
	}
	assert.Equal(t, want, got)
}
