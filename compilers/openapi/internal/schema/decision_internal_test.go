// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// TestRefinementOf_AnUnrefinedSchemaHoldsNothing pins that a schema whose allOf
// composes, or may not refine, gives its readers no refinement to read.
func TestRefinementOf_AnUnrefinedSchemaHoldsNothing(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Obj: {type: object, properties: {a: {type: string}}}
    T: {allOf: [{$ref: '#/components/schemas/Obj'}, {properties: {b: {type: string}}}]}
    U: {type: string}
`))
	for _, name := range []string{"T", "U"} {
		r, refined := refinementOf(l.Ctx, StatedIn(l, componentSchema(t, l, name)), jsontext.Pointer("/components/schemas/"+name))
		assert.False(t, refined, name)
		assert.Empty(t, r.comp.branches, "%s: no branch list is built", name)
		assert.Nil(t, r.principal, name)
	}
}

// TestEntryAt_ReadsTheBranchAtAnIndex pins the statement a reader of the
// decision with no pointer takes for an index, and none for -1.
func TestEntryAt_ReadsTheBranchAtAnIndex(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Port: {type: integer}
    T: {allOf: [{description: d}, {$ref: '#/components/schemas/Port'}]}
`))
	s := StatedIn(l, componentSchema(t, l, "T"))
	assert.Nil(t, entryAt(l.Ctx, s, -1))
	got := entryAt(l.Ctx, s, 1)
	require.NotNil(t, got)
	assert.Same(t, allOfBranches(l.Ctx, s, "").branches[1].schema, got)
	assert.NotNil(t, got.Ref, "the entry at 1 is the $ref")
}

// TestCarriedAt_ReadsWhereASchemaIsWritten pins which positions a carrier
// holds by where the bindings place the schema: a property a schema lists,
// and a parameter's or a header's schema, its content's included, but not a
// component, a list's element, a schema keyed `schema` beneath a component
// named as a carrier's collection is, nor a schema no binding places.
func TestCarriedAt_ReadsWhereASchemaIsWritten(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string}}
        - {name: c, in: query, content: {application/json: {schema: {type: string}}}}
      responses: {"204": {description: ok, headers: {X-A: {schema: {type: string}}}}}
components:
  schemas:
    H: {type: object, properties: {p: {type: string}}}
    L: {type: array, items: {type: string}}
    parameters: {type: object, $defs: {schema: {type: string}}}
    headers: {type: object, patternProperties: {schema: {type: string}}}
`)
	for at, want := range map[jsontext.Pointer]bool{
		"/components/schemas/H/properties/p":                           true,
		"/paths/~1x/get/parameters/0/schema":                           true,
		"/paths/~1x/get/parameters/1/content/application~1json/schema": true,
		"/paths/~1x/get/responses/204/headers/X-A/schema":              true,
		"/components/schemas/H":                                        false,
		"/components/schemas/L/items":                                  false,
		"/components/schemas/parameters/$defs/schema":                  false,
		"/components/schemas/headers/patternProperties/schema":         false,
	} {
		st := l.Ctx.Statements().At(bind.Position{Pointer: at})
		require.Equal(t, statement.Stated, st.Truth, "%s is written", at)
		assert.Equal(t, want, carriedAt(l.Ctx, st.Schema), at)
	}
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	assert.False(t, carriedAt(l.Ctx, StandaloneSchema(js.GetSchema())), "a schema no binding places is held by none")
}
