// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestOneOf_TheWarningOfADeadBranchSaysWhereTheUnionIs holds the one warning
// a schema with a dead branch draws to what is true of it: null satisfies a
// body admitting it beside a union taking it, by its one null branch or by
// any of them in an anyOf, and nothing satisfies a body keeping it out; a
// union on an allOf entry is judged against the rest of the allOf, which may
// admit more kinds than an object, and draws this warning only where the one
// of conjuncts sharing no value is silent, as where its null branch takes the
// null of a rest admitting it.
func TestOneOf_TheWarningOfADeadBranchSaysWhereTheUnionIs(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    Null:
      type: [object, 'null']
      oneOf: [{type: 'null'}, {type: integer}]
    Forbids:
      type: object
      properties: {p: {type: string}}
      oneOf: [{type: 'null'}, {type: integer}]
    AnyNulls:
      type: [object, 'null']
      properties: {p: {type: string}}
      anyOf: [{type: 'null'}, {type: integer}, {type: 'null'}]
    OneNulls:
      type: [object, 'null']
      properties: {p: {type: string}}
      oneOf: [{type: 'null'}, {type: integer}, {type: 'null'}]
    Entry:
      allOf: [{type: object}, {oneOf: [{$ref: '#/components/schemas/A'}, {type: boolean}]}]
    ON: {type: [object, 'null'], properties: {a: {type: string}}}
    Str: {type: string}
    EntryNull:
      allOf: [{$ref: '#/components/schemas/ON'}, {anyOf: [{$ref: '#/components/schemas/Str'}, {type: boolean}]}]
    EntryNullBranch:
      allOf: [{$ref: '#/components/schemas/ON'}, {oneOf: [{type: 'null'}, {$ref: '#/components/schemas/Str'}]}]
    EntryNoNull:
      allOf: [{$ref: '#/components/schemas/A'}, {oneOf: [{type: 'null'}, {$ref: '#/components/schemas/Str'}]}]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.NotNil(t, doc)
	for name, want := range map[string]string{
		"Null": "admits objects alone, null aside, and none of its 2 branches but a null one admits an object or " +
			"null (oneOf/1: integer), so no instance but null satisfies this schema",
		"Forbids": "admits objects alone, and none of its 2 branches admits an object (oneOf/1: integer), so no " +
			"instance satisfies this schema",
		"AnyNulls": "none of its 3 branches but its null ones admits an object or null (anyOf/1: integer), so no " +
			"instance but null satisfies this schema",
		"OneNulls": "none of its 3 branches but its null ones admits an object or null (oneOf/1: integer), so no " +
			"instance satisfies this schema",
		"Entry": "1 of the 2 branches of the oneOf on allOf/1 admits no value the rest of the allOf admits " +
			"(allOf/1/oneOf/1: boolean), so no instance satisfies it",
		"EntryNull": "the conjuncts of this allOf share no value (allOf/0: object, allOf/1: string or boolean)",
		"EntryNullBranch": "none of the 2 branches of the oneOf on allOf/1 but a null one admits a value the rest " +
			"of the allOf admits (allOf/1/oneOf/1: string), so no instance but null satisfies this schema",
		"EntryNoNull": "the conjuncts of this allOf share no value",
	} {
		reports := diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/"+name)
		var warned []string
		for _, d := range reports {
			if d.Severity == ir.SeverityWarning {
				warned = append(warned, d.Message)
			}
		}
		require.Len(t, warned, 1, name)
		assert.Contains(t, warned[0], want, name)
	}
}
