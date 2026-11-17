// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// keptOnceCases write, at each version, an object body admitting null beside
// a oneOf whose branch 0 is an object and whose other branches each admit an
// object among other kinds, so a variant keeps them; nullable says which of
// those admits null.
var keptOnceCases = []struct {
	version    string
	components []string
	nullable   []bool
}{
	{"3.1.0", []string{
		"B: {type: object, properties: {b: {type: integer}}}",
		"TL: {type: [object, string, 'null'], properties: {t: {type: string}}}",
		"N: {type: [object, 'null'], properties: {p: {type: string}}, oneOf: [{$ref: '#/components/schemas/B'}, " +
			"{required: [x]}, {anyOf: [{type: object, properties: {o: {type: string}}}, " +
			"{type: array, items: {type: object, properties: {i: {type: string}}}}, {type: 'null'}]}, " +
			"{$ref: '#/components/schemas/TL', minProperties: 2}]}",
	}, []bool{false, false, true, true}},
	{"3.0.3", []string{
		"B: {type: object, properties: {b: {type: integer}}}",
		"N: {type: object, nullable: true, properties: {p: {type: string}}, oneOf: [{$ref: '#/components/schemas/B'}, " +
			"{required: [x]}, {anyOf: [{type: object, properties: {o: {type: string}}}, " +
			"{type: array, items: {type: object, properties: {i: {type: string}}}}, {enum: [null]}]}]}",
	}, []bool{false, false, true}},
}

// TestCompile_ABranchAVariantKeepsLowersNowhereElse holds that a branch a
// distributed variant keeps verbatim is held there alone: no node stands at
// or under its pointer, the target of a $ref it writes stays a node of its
// own, the variant's reference admits null where the branch and the body both
// do, and the branch draws one info and no other finding. Every harness oracle
// passes, and the components lower alike in either order.
func TestCompile_ABranchAVariantKeepsLowersNowhereElse(t *testing.T) {
	t.Parallel()
	for _, tc := range keptOnceCases {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			spec := admissionSpec(tc.version, "", tc.components...)
			res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
			require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
			inBothMappingOrders(t, spec, func(t *testing.T, spec string) {
				doc, diags := compileAnnotationSpec(t, "kept", spec)
				requireErrorsAndWarnings(t, diags, nil)
				union, ok := kindNode(t, doc, componentID("N")).(*ir.Union)
				require.True(t, ok, "the body distributes over the union")
				require.Len(t, union.Variants, len(tc.nullable))
				for i := 1; i < len(tc.nullable); i++ {
					requireKeptOnce(t, doc, diags, union.Variants[i], i)
					assert.Equal(t, tc.nullable[i], union.Variants[i].Type.Nullable, "the reference to variant %d", i)
				}
				if tc.version == "3.1.0" {
					assert.IsType(t, &ir.Union{}, kindNode(t, doc, componentID("TL")), "the type list is its own union")
				}
			})
		})
	}
}

// requireKeptOnce holds the variant over branch i of N's oneOf to keeping
// the branch verbatim, with no node at or under the branch's pointer and one
// finding there, the info saying it is kept.
func requireKeptOnce(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, v ir.Variant, i int) {
	t.Helper()
	at := "/components/schemas/N/oneOf/" + strconv.Itoa(i)
	m, ok := doc.Types[v.Type.Target].(*ir.Model)
	require.True(t, ok, "the variant over branch %d is a model", i)
	assert.Contains(t, m.Unmodeled, "openapi:oneOf/"+strconv.Itoa(i))
	for id := range doc.Types {
		own := strings.TrimPrefix(string(id), "t/anon")
		assert.False(t, own == at || strings.HasPrefix(own, at+"/"), "%s holds branch %d again", id, i)
	}
	var found []string
	for _, d := range diags {
		p := string(d.Provenance.Pointer)
		if p == at || strings.HasPrefix(p, at+"/") {
			found = append(found, d.Code)
		}
	}
	assert.Equal(t, []string{diag.DegradedConstruct}, found, "the findings at and under %s", at)
}
