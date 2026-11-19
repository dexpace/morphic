// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/ir"
)

// TestLoad_ARootDefinitionResolvesAsTheResolverReadsIt pins GitHub #910: a
// "#/$defs/..." reference into a "$defs" written at the document's root
// resolves to the definition there, before an ancestor's of the same name, and
// a chain through it goes on from there, with no error reported. The bindings
// answer where each reference lands and where its chain ends.
func TestLoad_ARootDefinitionResolvesAsTheResolverReadsIt(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n" +
		"$defs:\n  n: {type: string}\n  hop: {$ref: '#/components/schemas/Z'}\n" +
		"components:\n  schemas:\n" +
		"    A: {type: object, $defs: {n: {type: integer}}, properties: {p: {$ref: '#/$defs/n'}, q: {$ref: '#/$defs/hop'}}}\n" +
		"    Z: {type: boolean}\n"

	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})

	require.NoError(t, err)
	require.NotNil(t, got)
	for _, diagnostic := range diags {
		assert.NotEqual(t, ir.SeverityError, diagnostic.Severity, "%s %s", diagnostic.Code, diagnostic.Message)
	}
	a, ok := got.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	for _, c := range []struct{ property, lands, ends string }{
		{"p", "/$defs/n", "/$defs/n"},
		{"q", "/$defs/hop", "/components/schemas/Z"},
	} {
		property, ok := a.GetSchema().GetProperties().Get(c.property)
		require.True(t, ok, c.property)
		entry, ok := got.Bindings.Entry(property)
		require.True(t, ok, "%s is bound", c.property)
		assert.Equal(t, reftable.Resolved, entry.Hop.Status, c.property)
		assert.Equal(t, reftable.DocumentPointer{Pointer: jsontext.Pointer(c.lands)}, entry.Hop.Target, "%s lands on the root's definition", c.property)
		assert.Equal(t, reftable.Resolved, entry.Chain.Status, c.property)
		assert.Equal(t, reftable.DocumentPointer{Pointer: jsontext.Pointer(c.ends)}, entry.Chain.End, "%s's chain goes on from there", c.property)
	}
	q, _ := a.GetSchema().GetProperties().Get("q")
	require.NotNil(t, got.Bindings.End(q))
	assert.Equal(t, []oas3.SchemaType{oas3.SchemaTypeBoolean}, got.Bindings.End(q).GetSchema().GetType(), "the chain goes on to Z")
}

// TestLoad_ARootDefinitionAnswersBeforeASchemasOwn pins GitHub #910 for a
// schema with no $id that spells "#/$defs/e" and defines an e of its own, where
// the root defines one too: the root's definition answers, as the rule for a
// definition states, however the two are ordered in the source and whether the
// $ref is reached first directly or by a chain through the schema. The binder
// reads neither definition by order, so the $ref resolves, where the parser's
// resolver reads one or the other by what it resolved first
// (docs/parser-library.md#root-defs).
func TestLoad_ARootDefinitionAnswersBeforeASchemasOwn(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
	for _, c := range []struct {
		name, schemas, root string
		// chained is the site of a $ref that reaches A's, if the row has one.
		chained string
	}{
		{"its own definition leads back to it",
			"components:\n  schemas:\n    A: {$ref: '#/$defs/e', $defs: {e: {$ref: '#/components/schemas/A'}}}\n",
			"$defs:\n  e: {type: object}\n", ""},
		{"a chain reaches it first",
			"components:\n  schemas:\n    C: {$ref: '#/components/schemas/A'}\n    A: {$ref: '#/$defs/e', $defs: {e: {type: integer}}}\n",
			"$defs:\n  e: {type: string}\n", "/components/schemas/C"},
	} {
		for _, order := range []struct{ name, spec string }{
			{"root last", head + c.schemas + c.root},
			{"root first", head + c.root + c.schemas},
		} {
			t.Run(c.name+", "+order.name, func(t *testing.T) {
				t.Parallel()
				got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(order.spec)}, Options{})

				require.NoError(t, err)
				require.NotNil(t, got, "%+v", diags)
				for _, diagnostic := range diags {
					assert.NotEqual(t, ir.SeverityError, diagnostic.Severity, "%s %s", diagnostic.Code, diagnostic.Message)
					assert.NotEqual(t, diag.UnresolvedRef, diagnostic.Code, diagnostic.Message)
					assert.NotEqual(t, diag.CyclicRef, diagnostic.Code, diagnostic.Message)
				}
				root := reftable.DocumentPointer{Pointer: "/$defs/e"}
				entry, ok := got.Bindings.Table().Entry(reftable.DocumentPointer{Pointer: "/components/schemas/A"})
				require.True(t, ok)
				assert.Equal(t, reftable.Resolved, entry.Hop.Status)
				assert.Equal(t, root, entry.Hop.Target, "the root's definition answers, not A's own")
				assert.Equal(t, root, entry.Chain.End)
				if c.chained == "" {
					return
				}
				via, ok := got.Bindings.Table().Entry(reftable.DocumentPointer{Pointer: jsontext.Pointer(c.chained)})
				require.True(t, ok)
				assert.Equal(t, root, via.Chain.End, "a chain through A ends on the root's definition too")
			})
		}
	}
}
