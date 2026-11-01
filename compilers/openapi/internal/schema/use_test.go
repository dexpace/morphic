// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestUse_ALoopOfNarrowingEnumsEnds pins the bound on what one use reads on
// through: enums each passing over a $ref to the next never end on a schema, so
// the walk stops, and what it read stands.
func TestUse_ALoopOfNarrowingEnumsEnds(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        f: {$ref: '#/components/schemas/A'}
    A: {enum: [a], allOf: [{$ref: '#/components/schemas/B'}]}
    B: {enum: [a], description: from b, allOf: [{$ref: '#/components/schemas/A'}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, "from b", propertyOf(t, doc, "Holder", "f").Docs.Description)
}

// TestUse_ADefaultReadsOnlyWhereThePositionTakesIt pins a default read across a
// narrowing at each spelling, by value and, with no value set, by kind.
func TestUse_ADefaultReadsOnlyWhereThePositionTakesIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        kept: {allOf: [{$ref: '#/components/schemas/Num'}], type: number}
        dropped: {allOf: [{$ref: '#/components/schemas/Num'}, {type: integer}]}
        viaRef: {$ref: '#/components/schemas/Narrow'}
    Num: {type: number, default: 1.5}
    Narrow: {allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [b]}]}
    Mode: {type: string, default: a}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.NotNil(t, propertyOf(t, doc, "Holder", "kept").Default)
	assert.Nil(t, propertyOf(t, doc, "Holder", "dropped").Default, "an integer takes no 1.5")
	assert.Nil(t, propertyOf(t, doc, "Holder", "viaRef").Default, "the enum a $ref lands on takes no a")

	unread, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Odd'}, {enum: [a]}]}
    Odd: {type: string, default: .nan}
`))
	assert.Nil(t, propertyOf(t, unread, "Holder", "f").Default, "a value it cannot read is none it takes")
}

// TestUse_ACarrierReadsThroughADecorator pins the object spelling of a read-only
// use: a decorator over an object keeps its node, and the carrier holding it reads
// what its $ref binds as the 3.1 spelling does. A composition adding a member says
// more than its $ref, so it reads nothing through it.
func TestUse_ACarrierReadsThroughADecorator(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", `    Stamp:
      type: object
      readOnly: true
      default: {at: 1}
      properties: {at: {type: integer}}
    Holder:
      type: object
      properties:
        wrapped: {allOf: [{$ref: '#/components/schemas/Stamp'}], description: mine}
        branch: {allOf: [{$ref: '#/components/schemas/Stamp'}, {description: mine}]}
        viaRef: {$ref: '#/components/schemas/Wrap'}
        adds:
          allOf:
            - {$ref: '#/components/schemas/Stamp'}
            - {properties: {extra: {type: string}}}
        requires: {allOf: [{$ref: '#/components/schemas/Stamp'}], required: [at]}
    Wrap: {allOf: [{$ref: '#/components/schemas/Stamp'}], description: wrapper}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, wire := range []string{"wrapped", "branch", "viaRef"} {
		p := propertyOf(t, doc, "Holder", wire)
		assert.NotEmpty(t, p.Visibility.Only, wire)
		assert.NotNil(t, p.Default, wire)
	}
	for _, wire := range []string{"adds", "requires"} {
		p := propertyOf(t, doc, "Holder", wire)
		assert.Empty(t, p.Visibility.Only, wire)
		assert.Nil(t, p.Default, wire)
	}
}

// TestUse_ANarrowingOfTwoRefsLendsNothing pins that an enum beside an allOf of two
// $refs reads neither: no precedence chooses one.
func TestUse_ANarrowingOfTwoRefsLendsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: string, description: a}
    B: {type: string, description: b}
    Holder:
      type: object
      properties:
        f: {enum: [x], allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	p := propertyOf(t, doc, "Holder", "f")
	assert.Empty(t, firstDocs(doc, p).Description)
}
