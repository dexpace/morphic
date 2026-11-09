// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestHeldAt_AContextPlacingNoPositionHoldsNothing pins what a tuple's entries
// read where the context reads another document's content at the source's
// positions, which the bindings place nowhere: no schema, rather than the
// source's at the same pointer.
func TestHeldAt_AContextPlacingNoPositionHoldsNothing(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{}).InScope(resolve.Scope{Foreign: true, Holder: "o.yaml"})
	_, placed := c.Position("/items/0")
	assert.False(t, placed, "sanity: the context places no position")
	assert.Nil(t, heldAt(c, "/items/0"))
}

// TestWrittenAt_ReadsWhereTheBindingsPlaceTheSchema pins where a tuple's
// entries are read: where the bindings place the schema writing them, as for a
// type list's array variant, which lowers at a pointer of its own, and at the
// pointer it lowers at where they place it nowhere.
func TestWrittenAt_ReadsWhereTheBindingsPlaceTheSchema(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#'
paths: {}
components:
  schemas:
    U: {type: [array, object], items: [{description: a}]}
`)
	js, ok := l.Ctx.Doc.Components.Schemas.Get("U")
	require.True(t, ok)
	_, at := writtenAt(l.Ctx, js.GetSchema(), "/components/schemas/U/type/0")
	assert.Equal(t, jsontext.Pointer("/components/schemas/U"), at)
	_, at = writtenAt(l.Ctx, &oas3.Schema{}, "/x")
	assert.Equal(t, jsontext.Pointer("/x"), at, "a schema the bindings place nowhere")
}

// TestListedTuple_ReadsADraftsEntriesAndRest pins what a part's reader takes
// for a tuple a draft writes as a list of items: the entries and the
// additionalItems the load phase built, through a `$ref` too, the rest nil
// where none is written, and nothing for a tuple of prefixItems or one the
// bindings place nowhere, as another compile's is.
func TestListedTuple_ReadsADraftsEntriesAndRest(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#'
paths: {}
components:
  schemas:
    Pair: {type: array, items: [{description: a}, {description: b}], additionalItems: {description: rest}}
    Ref: {$ref: '#/components/schemas/Pair'}
    Closed: {type: array, items: [{description: a}], additionalItems: false}
    Open: {type: array, items: [{description: a}]}
    Prefixed: {type: array, prefixItems: [{description: a}], items: false}
`)
	component := func(name string) *oas3.JSONSchema[oas3.Referenceable] {
		js, ok := l.Ctx.Doc.Components.Schemas.Get(name)
		require.True(t, ok, "sanity: %s is declared", name)
		return js
	}
	descriptions := func(entries []*oas3.JSONSchema[oas3.Referenceable]) []string {
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.GetSchema().GetDescription())
		}
		return out
	}
	for _, name := range []string{"Pair", "Ref"} {
		entries, rest, ok := ListedTuple(l.Ctx, component(name))
		require.True(t, ok, "%s is a tuple", name)
		assert.Equal(t, []string{"a", "b"}, descriptions(entries), "%s's entries", name)
		assert.Equal(t, "rest", rest.GetSchema().GetDescription(), "%s's rest", name)
	}
	entries, rest, ok := ListedTuple(l.Ctx, component("Closed"))
	require.True(t, ok, "a closed tuple is one")
	assert.Equal(t, []string{"a"}, descriptions(entries), "a closed tuple's entries")
	assert.True(t, annotation.IsFalseSchema(rest), "a closed tuple's rest admits nothing")
	entries, rest, ok = ListedTuple(l.Ctx, component("Open"))
	require.True(t, ok, "a tuple writing no rest is one")
	assert.Equal(t, []string{"a"}, descriptions(entries), "an open tuple's entries")
	assert.Nil(t, rest, "a tuple writing no rest has none")
	_, _, ok = ListedTuple(l.Ctx, component("Prefixed"))
	assert.False(t, ok, "a tuple of prefixItems is no list of items")
	other, _ := LoweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#'
paths: {}
components:
  schemas:
    Elsewhere: {type: array, items: [{description: a}]}
`)
	elsewhere, declared := other.Ctx.Doc.Components.Schemas.Get("Elsewhere")
	require.True(t, declared, "sanity: Elsewhere is declared")
	_, _, ok = ListedTuple(l.Ctx, elsewhere)
	assert.False(t, ok, "a tuple the bindings place nowhere is none")
}
