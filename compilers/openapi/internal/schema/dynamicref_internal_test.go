// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
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

// TestOneResource_SaysWhyAPositionMayLieInAnother pins the verdict's three
// answers: a schema in its document's own resource, one under an $id, and one a
// merge past the bound may hide an $id above, which must not read as none.
func TestOneResource_SaysWhyAPositionMayLieInAnother(t *testing.T) {
	t.Parallel()
	l, diags := LoweredFor(t, deepMergeSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	for at, want := range map[jsontext.Pointer]string{
		"/components/schemas/Plain":        "",
		"/components/schemas/Held/$defs/d": "starts a schema resource of its own",
		"/components/schemas/Deep":         "may hide an $id",
	} {
		why, ok := oneResource(l.Ctx, bind.Position{Pointer: at})
		assert.Equal(t, want == "", ok, at)
		assert.Contains(t, why, want, at)
	}
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

// TestAnchorHolds_NeedsASchemaInAnotherDocument pins the target a position of
// another document can be: one holding a schema to hoist.
func TestAnchorHolds_NeedsASchemaInAnotherDocument(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	why, ok := anchorHolds(l.Ctx, "n", bind.Position{Doc: 1, Pointer: "/T"})
	assert.False(t, ok)
	assert.Contains(t, why, "no schema could be built")
}
