// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestDeclaresResourceIDAbove_WithoutARawTree pins the guard on the path walk.
// The walk reads the raw source because $id has no oas3.Schema field, so a
// document with no raw tree behind it — one built in memory, or a pointer naming
// a position the source does not have — must read as "no resource boundary
// found" rather than wander off the tree.
//
// No committed spec reaches this: every pointer a $dynamicRef is lowered at was
// built by walking the very tree this re-walks. The guard is exercised directly
// because the hazard is a later caller passing a synthesized pointer, not a live
// defect.
func TestDeclaresResourceIDAbove_WithoutARawTree(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})

	assert.False(t, declaresResourceIDAbove(l.Ctx, "/components/schemas/A"),
		"a document with no raw tree declares no resource anywhere")
}

// TestDeclaresResourceIDAbove_EmptySegmentIsATokenNotAnArtifact pins which
// empty segments the path walk may drop. Splitting a pointer on '/' yields one
// leading empty segment no reference token stands behind; every later one names
// the key "", which is how a schema literally named "" is addressed. Dropping
// those would read the $id of the components/schemas map instead of the
// schema's own.
//
// The document root carries a member named "" with an $id, which makes the two
// rootward cases discriminating: the empty pointer names the root and must not
// reach it, while "/" is how ids.Ptr spells that member and must. Without the
// $id both read false.
func TestDeclaresResourceIDAbove_EmptySegmentIsATokenNotAnArtifact(t *testing.T) {
	t.Parallel()
	l, diags := LoweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
"": {$id: https://example.com/root-member}
components:
  schemas:
    "":
      $id: https://example.com/empty
      properties: {x: {type: string}}
    Sibling: {type: string}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	for name, tc := range map[string]struct {
		pointer jsontext.Pointer
		want    bool
		why     string
	}{
		"the position is named by a trailing empty token": {
			"/components/schemas/", true,
			"the trailing token names the schema itself, whose own $id is the boundary",
		},
		"an interior empty token still descends": {
			"/components/schemas//properties/x", true,
			"the $id above a property is still a boundary over it",
		},
		"a sibling sits above no $id at all": {
			"/components/schemas/Sibling", false,
			"a neighbour of the named schema inherits no boundary from it",
		},
		"the empty pointer names the document root": {
			"", false,
			`the empty pointer stops at the root, so the $id under the root's "" member is out of reach`,
		},
		"a lone slash names the root member keyed \"\"": {
			"/", true,
			`ids.Ptr("") spells that member "/", so its own $id is the boundary — reading "/" as the root, ` +
				"the way a reference resolves, would walk straight past it",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, declaresResourceIDAbove(l.Ctx, tc.pointer), tc.why)
		})
	}
}

// TestComponentSchemaAt_OnlyATopLevelComponentPointerHasABody pins the split
// the function exists for: a component pointer has a body, and a pointer into
// that same component does not.
//
// The fixture declares an empty-named component beside the real one, which
// makes the guard observable. A deeper pointer's component name comes back
// empty, so without the guard the lookup asks for "" and, in a document
// declaring no such name, misses and returns nil, which looks exactly like the
// right answer (#202).
func TestComponentSchemaAt_OnlyATopLevelComponentPointerHasABody(t *testing.T) {
	t.Parallel()
	l, diags := LoweredFor(t, openapitest.ComponentSpec(
		"    Outer:\n      type: object\n      title: outer\n"+
			"      properties: {inner: {type: string, title: inner}}\n"+
			"    \"\": {type: string, title: empty}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	tests := []struct {
		name, wantTitle string
		pointer         jsontext.Pointer
	}{
		{name: "the component itself", pointer: "/components/schemas/Outer", wantTitle: "outer"},
		// The empty name is a name the document may declare, but it is not one this
		// grammar addresses: a pointer ending at the slash names no entry, so the
		// component it declares is reached as an anonymous position instead.
		{name: "the pointer that names no entry", pointer: "/components/schemas/"},
		{name: "a property inside that component", pointer: "/components/schemas/Outer/properties/inner"},
		{name: "a component of another kind", pointer: "/components/parameters/Outer"},
		{name: "no component pointer at all", pointer: "/paths/~1a/get"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := componentSchemaAt(l.Ctx, tc.pointer)
			if tc.wantTitle == "" {
				assert.Nil(t, got, "only a top-level component schema has a body here")
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.wantTitle, got.GetTitle(), "and it is that component's own body")
		})
	}
}

// TestDynamicHop_HopsOnlyWhenExactlyOneAnchorSiteIsNamed pins every way the
// chain walk can stop, since a wrong verdict either expands what it should
// refuse or refuses what it should expand (#202).
//
// A hop needs a component at the pointer, a $dynamicRef written on it, and
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
			name:    "the pointer names no component",
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
				sites, siteDiags := l.Ctx.DynamicAnchorSites(tc.anchor)
				require.Len(t, sites, tc.wantSites, "the fixture must set up the case it is named for")
				require.Empty(t, siteDiags)
			}

			next, hops, hopDiags := dynamicHop(l.Ctx, tc.at)

			assert.Equal(t, tc.wantNext != "", hops)
			assert.Equal(t, tc.wantNext, next)
			assert.Empty(t, hopDiags)
		})
	}
}
