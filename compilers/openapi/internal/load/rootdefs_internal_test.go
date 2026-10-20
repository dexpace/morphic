package load

import (
	"encoding/json/jsontext"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "%s %s", d.Code, d.Message)
	}
	a, ok := got.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	for _, c := range []struct{ prop, lands, ends string }{
		{"p", "/$defs/n", "/$defs/n"},
		{"q", "/$defs/hop", "/components/schemas/Z"},
	} {
		prop, ok := a.GetSchema().GetProperties().Get(c.prop)
		require.True(t, ok, c.prop)
		entry, ok := got.Bindings.Entry(prop)
		require.True(t, ok, "%s is bound", c.prop)
		assert.Equal(t, bind.Resolved, entry.Hop.Status, c.prop)
		assert.Equal(t, bind.Position{Pointer: jsontext.Pointer(c.lands)}, entry.Hop.Target, "%s lands on the root's definition", c.prop)
		assert.Equal(t, bind.Resolved, entry.Chain.Status, c.prop)
		assert.Equal(t, bind.Position{Pointer: jsontext.Pointer(c.ends)}, entry.Chain.End, "%s's chain goes on from there", c.prop)
	}
	q, _ := a.GetSchema().GetProperties().Get("q")
	require.NotNil(t, got.Bindings.End(q))
	assert.Equal(t, []oas3.SchemaType{oas3.SchemaTypeBoolean}, got.Bindings.End(q).GetSchema().GetType(), "the chain goes on to Z")
}

// TestLoad_ARootDefinitionAnswersBeforeASchemasOwn pins GitHub #910 for a
// schema with no $id that spells "#/$defs/e" and defines an e of its own, where
// the root defines one too: the root's definition answers, as the rule for a
// definition states, however the two are ordered in the source and whether the
// $ref is reached first directly or by a chain through the schema. The
// compiler once left such a $ref unresolved, because the parser's resolver
// read one definition or the other by what it had resolved first; the binder
// reads neither by order, so the refusal is lifted.
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
				for _, d := range diags {
					assert.NotEqual(t, ir.SeverityError, d.Severity, "%s %s", d.Code, d.Message)
					assert.NotEqual(t, diag.UnresolvedRef, d.Code, d.Message)
					assert.NotEqual(t, diag.CyclicRef, d.Code, d.Message)
				}
				root := bind.Position{Pointer: "/$defs/e"}
				entry, ok := got.Bindings.Table().Entry(bind.Position{Pointer: "/components/schemas/A"})
				require.True(t, ok)
				assert.Equal(t, bind.Resolved, entry.Hop.Status)
				assert.Equal(t, root, entry.Hop.Target, "the root's definition answers, not A's own")
				assert.Equal(t, root, entry.Chain.End)
				if c.chained == "" {
					return
				}
				via, ok := got.Bindings.Table().Entry(bind.Position{Pointer: jsontext.Pointer(c.chained)})
				require.True(t, ok)
				assert.Equal(t, root, via.Chain.End, "a chain through A ends on the root's definition too")
			})
		}
	}
}
