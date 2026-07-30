package openapi

import (
	"reflect"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/ir"
)

// docDeclaring builds a document declaring the named component schemas, with no
// parser and no fixture — which is the point of deriving the index from the
// document rather than from a lowering pass.
func docDeclaring(names ...string) *soa.OpenAPI {
	elems := make([]*sequencedmap.Element[string, *oas3.JSONSchema[oas3.Referenceable]], 0, len(names))
	for _, n := range names {
		elems = append(elems, sequencedmap.NewElem(n,
			oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})))
	}
	return &soa.OpenAPI{Components: &soa.Components{Schemas: sequencedmap.New(elems...)}}
}

// TestLowerCtx_HasNoExportedMap is the guard that makes "immutable by value" true
// rather than conventional. A struct copy shares a map rather than copying it,
// so an exported map field would be the one part of the context a callee could
// write to — and the write would be visible to its caller's caller, which is
// exactly the class of bug passing by value is meant to remove.
//
// Slices are held to the same rule for the same reason: a copy shares the
// backing array. The exported pointer to the document is deliberately not
// covered — it is shared by design and lowering never writes through it, which
// TestNewLowerCtx_KeepsTheDocumentItWasGiven pins.
func TestLowerCtx_HasNoExportedMap(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeOf(lowerCtx{})
	require.Positive(t, rt.NumField(), "a context with no fields would pass this vacuously")

	var checked int
	for i := range rt.NumField() {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		checked++
		assert.NotEqual(t, reflect.Map, f.Type.Kind(),
			"exported field %s is a map, so a copy of the context shares it", f.Name)
		assert.NotEqual(t, reflect.Slice, f.Type.Kind(),
			"exported field %s is a slice, so a copy of the context shares its backing array", f.Name)
	}
	assert.Positive(t, checked, "no exported field was examined, so this asserted nothing")
}

// TestNewLowerCtx_DerivesTheDeclaredSchemaNames pins the index the $ref and
// discriminator-mapping resolutions read. A name missing from it is not a
// resolvable target, so under-deriving turns a valid $ref into a dangling one;
// over-deriving mints an ID for a component the document never declared.
func TestNewLowerCtx_DerivesTheDeclaredSchemaNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		doc      *soa.OpenAPI
		declares []string
		denies   []string
	}{
		{
			name: "every declared component", doc: docDeclaring("User", "Order", "A~B"),
			declares: []string{"User", "Order", "A~B"}, denies: []string{"Missing", ""},
		},
		{
			name: "a document declaring none", doc: &soa.OpenAPI{},
			denies: []string{"User", ""},
		},
		{
			name: "components present but no schemas", doc: &soa.OpenAPI{Components: &soa.Components{}},
			denies: []string{"User"},
		},
		{
			name: "the empty name is a name like any other", doc: docDeclaring(""),
			declares: []string{""}, denies: []string{"User"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newLowerCtx(0, &load.Document{Doc: tc.doc}, Options{})
			for _, n := range tc.declares {
				assert.True(t, c.DeclaresSchema(n), "%q is declared", n)
			}
			for _, n := range tc.denies {
				assert.False(t, c.DeclaresSchema(n), "%q is not declared", n)
			}
		})
	}
}

// TestDeclaresSchema_TheZeroContextDeclaresNothing pins the nil-index read. The
// index is nil for a document with no components, so the accessor must answer
// on a nil map rather than assume one was built.
func TestDeclaresSchema_TheZeroContextDeclaresNothing(t *testing.T) {
	t.Parallel()
	var c lowerCtx
	assert.False(t, c.DeclaresSchema("User"))
	assert.False(t, c.DeclaresSchema(""))
}

// TestNewLowerCtx_KeepsTheDocumentItWasGiven pins the rest of the context: the
// document, options, source identity and index arrive unchanged, since every
// Provenance the compile stamps is built from the last two.
func TestNewLowerCtx_KeepsTheDocumentItWasGiven(t *testing.T) {
	t.Parallel()
	doc := docDeclaring("User")
	src := ir.SourceInfo{Format: "openapi@3.1", Path: "spec.yaml", Hash: "abc"}
	opts := Options{Grouping: GroupByPathPrefix, DisableExternalRefs: true}

	c := newLowerCtx(7, &load.Document{Doc: doc, Source: src}, opts)

	assert.Same(t, doc, c.Doc, "the document is referenced, never copied")
	assert.Equal(t, src, c.Source)
	assert.Equal(t, opts, c.Opts)
	assert.Equal(t, 7, c.SrcIndex)
}
