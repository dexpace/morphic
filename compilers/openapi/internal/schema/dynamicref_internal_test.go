// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// deepMergeSpec holds a schema in its document's resource, one under an $id, and
// one merging through a chain past the view's bound, built rather than spelled
// since it is 70 links long.
var deepMergeSpec = func() string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-c0: &c0 {$id: 'https://x.test/deep'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	b.WriteString("components:\n  schemas:\n    Plain: {type: string}\n")
	b.WriteString("    Held: {$id: 'https://x.test/held', $defs: {d: {type: string}}}\n    Deep: {<<: *c70}\n")
	return b.String()
}()

// TestSameResource_SaysWhyTwoPositionsMayLieInTwo pins the verdict's three
// answers: two schemas in one resource, their document's or one an $id starts,
// two an $id between puts in two, and one a merge past the bound may hide an
// identifier above, which must not read as none.
func TestSameResource_SaysWhyTwoPositionsMayLieInTwo(t *testing.T) {
	t.Parallel()
	l, diags := LoweredFor(t, deepMergeSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	for _, tc := range []struct {
		from, at jsontext.Pointer
		want     string
	}{
		{"/components/schemas/Plain", "/components/schemas/Plain", ""},
		{"/components/schemas/Held/$defs/d", "/components/schemas/Held", ""},
		{"/components/schemas/Plain", "/components/schemas/Held/$defs/d", "puts the two in different schema resources"},
		{"/components/schemas/Held", "/components/schemas/Plain", "puts the two in different schema resources"},
		{"/components/schemas/Deep", "/components/schemas/Plain", "may hide an identifier"},
		{"/components/schemas/Plain", "/components/schemas/Deep", "may hide an identifier"},
	} {
		why, ok := sameResource(l.Ctx, bind.Position{Pointer: tc.from}, bind.Position{Pointer: tc.at})
		assert.Equal(t, tc.want == "", ok, "%s %s", tc.from, tc.at)
		assert.Contains(t, why, tc.want, "%s %s", tc.from, tc.at)
	}
	two, diags := LoweredFor(t, openapitest.ComponentSpec("    A: {$id: 'https://x.test/a', $defs: {d: {}}}\n"+
		"    B: {$id: 'https://x.test/b', $defs: {d: {}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	_, ok := sameResource(two.Ctx, bind.Position{Pointer: "/components/schemas/A/$defs/d"},
		bind.Position{Pointer: "/components/schemas/B/$defs/d"})
	assert.False(t, ok, "two resources an $id each starts are two")
}

// TestDynamicHop_HopsOnlyWhenExactlyOneAnchorSiteIsNamed pins every way the
// chain walk can stop, since a wrong verdict either expands what it should
// refuse or refuses what it should expand (#202).
//
// A hop needs a schema at the position, a $dynamicRef written on it, and
// exactly one site declaring the anchor it names. Anything else ends the chain,
// and a chain that ends has no cycle and expands, which is the direction sites
// already errs in. Hopping on any other count would pick an arbitrary branch,
// reporting a cycle the document does not have, or index an empty slice.
func TestDynamicHop_HopsOnlyWhenExactlyOneAnchorSiteIsNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, schemas, anchor string
		at                    jsontext.Pointer
		wantSites             int
		wantNext              jsontext.Pointer
	}{
		{
			name: "the anchor is declared twice, so it names no single target",
			schemas: "    A: {$dynamicAnchor: m, type: string}\n" +
				"    B: {$dynamicAnchor: m, type: integer}\n" +
				"    C: {$dynamicRef: '#m'}\n",
			at: "/components/schemas/C", anchor: "m", wantSites: 2,
		},
		{
			name:    "the anchor is declared nowhere, so it names nothing",
			schemas: "    C: {$dynamicRef: '#ghost'}\n",
			at:      "/components/schemas/C", anchor: "ghost", wantSites: 0,
		},
		{
			name:    "the component writes no $dynamicRef at all",
			schemas: "    C: {type: string}\n",
			at:      "/components/schemas/C",
		},
		{
			name:    "the position holds no schema",
			schemas: "    C: {type: string}\n",
			at:      "/components/schemas/C/properties/inner",
		},
		{
			name: "declared once, which is the only shape that hops",
			schemas: "    A: {$dynamicAnchor: m, type: string}\n" +
				"    C: {$dynamicRef: '#m'}\n",
			at: "/components/schemas/C", anchor: "m", wantSites: 1,
			wantNext: "/components/schemas/A",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, diags := LoweredFor(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)
			if tc.anchor != "" {
				sites, _ := l.Ctx.DynamicAnchorSites(tc.anchor)
				require.Len(t, sites, tc.wantSites, "the fixture must set up the case it is named for")
			}

			next, hops := dynamicHop(l.Ctx, bind.Position{Pointer: tc.at})

			assert.Equal(t, tc.wantNext != "", hops)
			assert.Equal(t, tc.wantNext, next.Pointer)
		})
	}
}

// TestDynamicExpansion_ReadsNoAnchorsForContentNoDocumentPlaces pins that content
// read at the source's coordinates with no position of its document expands
// against nothing, the source's anchors included.
func TestDynamicExpansion_ReadsNoAnchorsForContentNoDocumentPlaces(t *testing.T) {
	t.Parallel()
	l, diags := LoweredFor(t, openapitest.ComponentSpec("    Node: {$dynamicAnchor: n, type: object}\n    D: {$dynamicRef: '#n'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	js, ok := l.Ctx.Doc.GetComponents().GetSchemas().Get("D")
	require.True(t, ok)

	_, _, expanded := dynamicExpansion(l.Ctx, js.GetSchema(), "/components/schemas/D")
	require.True(t, expanded, "read in the source it expands")

	foreign := l.Ctx.InScope(resolve.Scope{Foreign: true, Holder: "other.yaml"})
	_, why, expanded := dynamicExpansion(foreign, js.GetSchema(), "/components/schemas/D")
	assert.False(t, expanded)
	assert.Contains(t, why, "no position of its document")
}

// TestResolvesAt_AnswersAsResolvePointerDoes pins that the verdict an
// expansion asks is resolvePointer's own, at every landing the bindings of the
// dynamic-ref and schema-identifiers cases make, and at each site and target
// of their $refs.
func TestResolvesAt_AnswersAsResolvePointerDoes(t *testing.T) {
	t.Parallel()
	asked := 0
	for _, name := range []string{"dynamic-ref", "schema-identifiers"} {
		data, err := os.ReadFile(filepath.Join("../../../../testdata/conformance/openapi", name+".yaml"))
		require.NoError(t, err)
		l, _ := LoweredFor(t, string(data))
		table := l.Ctx.Bindings().Table()
		positions := []bind.Position{}
		for _, landing := range table.Landings() {
			positions = append(positions, landing.At)
		}
		for _, e := range table.Entries() {
			positions = append(positions, e.Site, e.Hop.Target)
		}
		for _, at := range positions {
			there, read := lowering.InDocument(l.Ctx, at.Doc)
			want := false
			if read {
				_, want = resolvePointer(there, l.Types, at.Pointer, l.Ctx.Bindings().At(at))
			}
			assert.Equal(t, want, resolvesAt(l.Ctx, at), "%s: %v", name, at)
			asked++
		}
	}
	assert.Positive(t, asked)
}

// TestBases_AnUnknownBaseIsNoMatch pins the three readers comparing bases where
// a $ref is written alike: past the merge bound a schema on the way may hide an
// $id, so a base the bindings cannot tell matches none, though both would read
// as the document's. Twin, written as Plain is in the document's resource,
// matches it at each. Deep's own $refs are reported unresolved, for the same
// reason, and nothing else is.
func TestBases_AnUnknownBaseIsNoMatch(t *testing.T) {
	t.Parallel()
	const body = "\n      type: object\n      properties: {p: {type: array, items: {$ref: '#/components/schemas/Item'}}}\n" +
		"      allOf: [{not: {$ref: '#/components/schemas/Item'}}]\n"
	src := strings.Replace(deepMergeSpec, "    Deep: {<<: *c70}\n",
		"    Item: {type: string}\n    Plain:"+body+"    Twin:"+body+"    Deep:\n      <<: *c70"+body, 1)
	l, diags := LoweredFor(t, src)
	var unresolved []jsontext.Pointer
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			unresolved = append(unresolved, d.Provenance.Pointer)
		}
	}
	assert.ElementsMatch(t, []jsontext.Pointer{"/components/schemas/Deep/properties/p/items",
		"/components/schemas/Deep/allOf/0/not"}, unresolved, "%+v", diags)
	at := func(name string) *oas3.JSONSchema[oas3.Referenceable] {
		js := l.Ctx.Bindings().At(bind.Position{Pointer: jsontext.Pointer("/components/schemas/" + name + "/properties/p")})
		require.NotNil(t, js, name)
		return js
	}
	branch := func(name string) allOfBranch {
		s := l.Ctx.Bindings().At(bind.Position{Pointer: jsontext.Pointer("/components/schemas/" + name)}).GetSchema()
		return allOfBranches(s, "").branches[0]
	}
	prov := func(name string) ir.Provenance {
		return l.Ctx.Place().At(jsontext.Pointer("/components/schemas/" + name + "/properties/p"))
	}
	w := newAdmission(l.Ctx)
	fields := modelFields{c: l.Ctx}

	assert.True(t, w.sameText(at("Plain"), at("Twin")))
	assert.False(t, w.sameText(at("Plain"), at("Deep")))
	assert.True(t, readsAtPosition(l.Ctx, branch("Plain")))
	assert.False(t, readsAtPosition(l.Ctx, branch("Deep")))
	assert.True(t, fields.Alike(prov("Plain"), prov("Twin")))
	assert.False(t, fields.Alike(prov("Plain"), prov("Deep")))
}

// TestUnresolvedRoot_SaysWhatHoldsNoSchema pins why a $recursiveRef whose
// resource's root holds no schema is kept: at a document's root, what an
// identifier would change, and elsewhere, where it is.
func TestUnresolvedRoot_SaysWhatHoldsNoSchema(t *testing.T) {
	t.Parallel()
	assert.Contains(t, unresolvedRoot(bind.Position{}), "under an identifier of its own")
	assert.Equal(t, `"#" names "/x", where no schema could be built`, unresolvedRoot(bind.Position{Doc: 1, Pointer: "/x"}))
}

// TestResolvesAt_ResolvesNothingInADocumentTheCompileDidNotRead pins the verdict
// at a position of a document the bindings read and the compile does not list
// beside the source: no reference resolves there, as resolveAt reads it.
func TestResolvesAt_ResolvesNothingInADocumentTheCompileDidNotRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte("X: {type: string}\n"), 0o600))
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(openapitest.ComponentSpec("    A: {$ref: './ext.yaml#/X'}\n"))}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	read := LowererOver(lowering.New(0, doc, lowering.Policy{}))
	e, ok := read.Ctx.Bindings().Table().Entry(bind.Position{Pointer: "/components/schemas/A"})
	require.True(t, ok)
	require.True(t, resolvesAt(read.Ctx, e.Hop.Target), "sanity: the document the compile read resolves")
	none := *doc
	none.Others = load.Others{}
	assert.False(t, resolvesAt(LowererOver(lowering.New(0, &none, lowering.Policy{})).Ctx, e.Hop.Target))
}
