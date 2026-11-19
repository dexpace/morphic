// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestUnion_APassedOverUnionSharingNoKindWithTheBodyKeepsBoth holds a oneOf
// and an anyOf whose union passed over shares no kind with the body to the
// Model the body gives, keeping both, and one warning that its conjuncts share
// no value: beside a body writing no allOf, which names its own type and the
// union, beside an allOf, which the composition's report names, and on two of
// its entries. A oneOf of null alone, kept as validation logic, is a conjunct
// admitting null and named so.
func TestUnion_APassedOverUnionSharingNoKindWithTheBodyKeepsBoth(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.1.0", `    T: {type: object, properties: {a: {type: string}}}
    Flat:
      type: object
      properties: {a: {type: string}}
      oneOf: [{type: string}, {type: integer}]
      anyOf: [{type: string}, {type: integer}]
    Beside:
      allOf: [{$ref: '#/components/schemas/T'}]
      oneOf: [{type: object, properties: {b: {type: string}}}]
      anyOf: [{type: string}, {type: integer}]
    NulBeside:
      allOf: [{$ref: '#/components/schemas/T'}]
      oneOf: [{type: 'null'}]
      anyOf: [{type: object}]
    TwoEntries:
      allOf:
        - {$ref: '#/components/schemas/T'}
        - {oneOf: [{type: object, properties: {b: {type: string}}}, {type: string}]}
        - {anyOf: [{type: string}, {type: integer}], allOf: [{required: [a]}]}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.NotNil(t, doc)
	for name, want := range map[string]struct {
		words  string
		reason ir.UnmodeledReason
	}{
		"Flat":      {"share no value (type: object, anyOf: string or integer), so no instance satisfies this schema, which its body alone", ir.ReasonDegradedLowering},
		"Beside":    {"share no value (anyOf: string or integer, allOf/0: object)", ir.ReasonDegradedLowering},
		"NulBeside": {"share no value (oneOf: null, anyOf: object, allOf/0: object)", ir.ReasonValidationOnly},
		"TwoEntries": {"share no value (allOf/0: object, allOf/1: object or string, allOf/2: string or integer)",
			ir.ReasonDegradedLowering},
	} {
		var warned []string
		for _, d := range diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/"+name) {
			if d.Severity == ir.SeverityWarning {
				warned = append(warned, d.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], want.words, name)
		m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/"+name)].(*ir.Model)
		require.True(t, ok, "%s is the Model its body gives", name)
		for _, key := range []string{"openapi:oneOf", "openapi:anyOf"} {
			assert.Equal(t, want.reason, m.Unmodeled[key].Reason, "%s keeps %s", name, key)
		}
	}
}

// TestAllOf_ANullReadPastItsBudgetIsDecidedByEachConjunct holds the report of
// conjuncts sharing no value, where the read of null passes the walk's budget,
// to each conjunct read on its own: the keywords beside the allOf keep null
// out by their own `type`, whatever the union of more strings than the walk
// reads admits, so the schema is reported.
func TestAllOf_ANullReadPastItsBudgetIsDecidedByEachConjunct(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("    S: {type: string}\n    T:\n      anyOf:\n")
	for range 300 {
		b.WriteString("        - {$ref: '#/components/schemas/S'}\n")
	}
	b.WriteString("    P: {type: object, properties: {u: {type: string}}, allOf: [{$ref: '#/components/schemas/T'}]}\n")
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", b.String()))
	var warned []string
	for _, d := range diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/P") {
		if d.Severity == ir.SeverityWarning {
			warned = append(warned, d.Message)
		}
	}
	require.Len(t, warned, 1)
	assert.Contains(t, warned[0], "share no value (type: object, allOf/0: string)")
}
