// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestScopeOf_AChainPastTheBoundIsReadAsNoDocuments pins the scope of an entry
// whose chain outruns maxRefChain: none, so no reference in what it reaches
// reads as internal. Taken to end at the last hop read, it was read in that
// hop's document, which need not hold the end (GitHub #762).
func TestScopeOf_AChainPastTheBoundIsReadAsNoDocuments(t *testing.T) {
	t.Parallel()
	for hops, foreign := range map[int]bool{resolve.MaxRefChain - 1: false, resolve.MaxRefChain + 4: true} {
		doc, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(chainedAliasSpec(hops)), load.Options{})
		require.NoError(t, err)
		require.NotNil(t, doc)
		op, ok := doc.Doc.Paths.Get("/a")
		require.True(t, ok)
		params := op.GetObject().Get().GetParameters()
		require.Len(t, params, 1)

		scope := resolve.ScopeOf[soa.Parameter](resolve.Scope{Doc: doc.Doc, SelfPath: doc.Source.Path}, params[0])

		assert.Equal(t, foreign, scope.Foreign, "%d hops", hops)
		assert.Empty(t, scope.Holder, "%d hops", hops)
	}
}

// TestScopeOf pins the scope each entry's object is read in, given a Foreign
// scope over another document, which tells every answer apart. One whose chain
// ends in ext.yaml is read there. One whose chain ends in the source, however
// it got there, is read as the source's. One written inline, and one that
// resolved nothing, keep the scope given.
func TestScopeOf(t *testing.T) {
	t.Parallel()
	doc, ext := loadElsewhere(t)
	given := resolve.Scope{Doc: doc.Doc, Foreign: true, Holder: "other.yaml"}

	for path, want := range map[string]string{
		"/inline": "other.yaml", "/internal": "", "/ext": ext, "/back": "", "/hop": "", "/missing": "other.yaml",
	} {
		rp, ok := doc.Doc.Paths.Get(path)
		require.True(t, ok, path)
		scope := resolve.ScopeOf[soa.PathItem](given, rp)
		assert.Equal(t, want, scope.Holder, path)
		assert.Equal(t, want != "", scope.Foreign, path)
		assert.Same(t, doc.Doc, scope.Doc, path)
	}
}

// pathItemEnds is the Ends of a scope whose walks meet path items alone, as
// those through elsewhereRoot do.
func pathItemEnds(node any) (resolve.End, bool) {
	rp, ok := node.(*soa.ReferencedPathItem)
	if !ok {
		return resolve.End{}, false
	}
	return resolve.EndOf[soa.PathItem](rp)
}

// TestScopeOf_PastARefReadsWhereItsChainEnds pins the scope of an entry whose
// pointer passes a $ref, which the resolver's walk follows to what it resolved
// to, though it reports the source as what the pointer resolved against. Past
// /ext's, the response is ext.yaml's, read as /ext's own entry reads it, and
// ObjectAt names it by the pointer /ext's own lowering reaches it at, so both
// lowerings read one position alike (GitHub #762). Past /back's or
// /internal's, whose chains end in the source, it is the source's.
func TestScopeOf_PastARefReadsWhereItsChainEnds(t *testing.T) {
	t.Parallel()
	doc, ext := loadElsewhere(t)
	given := resolve.Scope{SelfPath: doc.Source.Path, Doc: doc.Doc, Foreign: true, Holder: "other.yaml",
		Ends: pathItemEnds}
	through, ok := doc.Doc.Paths.Get("/through")
	require.True(t, ok)
	responses := through.GetObject().Get().GetResponses()
	rp, ok := doc.Doc.Paths.Get("/ext")
	require.True(t, ok)
	own := resolve.ScopeOf[soa.PathItem](given, rp)
	require.Equal(t, ext, own.Holder, "/ext's own entry is read where ext.yaml sits")

	for code, want := range map[string]string{"200": ext, "201": "", "202": ""} {
		rr, ok := responses.Get(code)
		require.True(t, ok, code)
		require.NotNil(t, resolve.Object[soa.Response](rr), "%s: the resolver walked through the $ref", code)
		scope := resolve.ScopeOf[soa.Response](given, rr)
		assert.Equal(t, want, scope.Holder, code)
		assert.Equal(t, want != "", scope.Foreign, code)
	}
	rr, ok := responses.Get("200")
	require.True(t, ok)
	_, pointer := resolve.ObjectAt[soa.Response](given.InSource(), rr, "/paths/~1through/get/responses/200")
	assert.Equal(t, jsontext.Pointer("/paths/~1ext/get/responses/200"), pointer,
		"named where /ext's own lowering reaches it, in the scope that lowering reads it in")
}

// TestReferenceEnd_NamesWhereEachKindOfReferenceEnds pins ReferenceEnd for a
// resolved reference of each kind the library resolves, reached as a walk meets
// one, as a value of any type: each ends at the component it names, in the
// source, and nothing else is a reference. A schema's $ref is no such kind any
// more: the compiler resolves it (internal/bind) and the library never does, so
// the one the document writes ends nowhere the library read. Driving the
// compiler is what makes the resolution info real (see parseFull).
func TestReferenceEnd_NamesWhereEachKindOfReferenceEnds(t *testing.T) {
	t.Parallel()
	got, diags, err := load.Load(t.Context(), 0, openapitest.SourceOf(openapitest.EveryKindOfReference), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)

	ends := map[jsontext.Pointer]bool{}
	schemaRefs := 0
	for item := range soa.Walk(t.Context(), got.Doc) {
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			if js, ok := model.(*oas3.JSONSchema[oas3.Referenceable]); ok && js.IsReference() {
				schemaRefs++
				_, ended := resolve.ReferenceEnd(js)
				assert.False(t, ended, "the library resolved no schema $ref")
				return nil
			}
			if end, ok := resolve.ReferenceEnd(model); ok {
				assert.Same(t, got.Doc, end.Document, "%T ends in the source's model", model)
				ends[end.Pointer] = true
			}
			return nil
		}})
	}

	assert.Equal(t, map[jsontext.Pointer]bool{
		"/components/pathItems/P": true, "/components/parameters/Q": true, "/components/requestBodies/B": true,
		"/components/callbacks/C": true, "/components/headers/H": true, "/components/links/L": true,
		"/components/examples/E": true, "/components/responses/R": true, "/components/securitySchemes/K": true,
	}, ends)
	assert.Positive(t, schemaRefs, "the document writes a schema $ref")
	_, ok := resolve.ReferenceEnd(got.Doc)
	assert.False(t, ok, "a document is no reference")
}
