// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package provenance_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// TestAttribution_IndexOfNamesTheSourceThatWroteEveryPosition pins the rule for
// a construct no single position addresses: the one source that supplied every
// position it combines, else the answer for the position it is recorded at.
// The fallback is neither the overlay's index nor zero, so an answer that
// ignored it could not pass for the base document's.
func TestAttribution_IndexOfNamesTheSourceThatWroteEveryPosition(t *testing.T) {
	t.Parallel()
	const base = 7
	attributed := provenance.Attributed(1, map[jsontext.Pointer]bool{
		"/s/if": true, "/s/then": true, "/t": true, "/t/if": true,
	}, nil)
	tests := []struct {
		name        string
		attribution provenance.Attribution
		pointer     jsontext.Pointer
		from        []jsontext.Pointer
		want        int
	}{
		{name: "no positions is the pointer's own answer", attribution: attributed, pointer: "/t", want: 1},
		{name: "no positions, base-owned pointer", attribution: attributed, pointer: "/s", want: base},
		{name: "every position the overlay's", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/if", "/s/then"}, want: 1},
		{name: "positions from both documents", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/if", "/s/else"}, want: base},
		{name: "positions from both documents, the overlay's last", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/else", "/s/if"}, want: base},
		{name: "every position the base's", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/else"}, want: base},
		{name: "an overlay-introduced declaring position", attribution: attributed, pointer: "/t", from: []jsontext.Pointer{"/t/if"}, want: 1},
		{name: "no overlay applied", pointer: "/s", from: []jsontext.Pointer{"/s/if"}, want: base},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.attribution.IndexOf(tc.pointer, tc.from, base))
		})
	}
}

// TestDoc_AtPlacesAPointerInTheDocumentThatSuppliedIt pins Doc.At over the
// attribution it carries: a pointer the overlay supplied names the overlay, any
// other names the document, and a record assembled from several positions names
// the overlay only where it supplied all of them.
func TestDoc_AtPlacesAPointerInTheDocumentThatSuppliedIt(t *testing.T) {
	t.Parallel()
	doc := provenance.Doc{Source: 4, Overlay: provenance.Attributed(5, map[jsontext.Pointer]bool{
		"/a": true, "/s/if": true, "/s/then": true,
	}, nil)}

	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "/a"}, doc.At("/a"))
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/b"}, doc.At("/b"))
	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "/s"}, doc.At("/s", "/s/if", "/s/then"))
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/s"}, doc.At("/s", "/s/if", "/s/else"))
	assert.Equal(t, ir.Provenance{Pointer: "/a"}, provenance.Doc{}.At("/a"), "the zero Doc places in source 0")
}

// TestTree_NodePlacesANodeAndTheDocumentForNone pins the tree with no pointers
// and no overlay: a placed node is reported at its own line and column, a node
// the parser never placed carries no position, and no node at all names the
// document alone, never a position fabricated from a nil.
func TestTree_NodePlacesANodeAndTheDocumentForNone(t *testing.T) {
	t.Parallel()
	tree := provenance.Tree{Doc: provenance.Doc{Source: 4}}

	tests := map[string]struct {
		node *yaml.Node
		want ir.Provenance
	}{
		"a placed node reports its line and column": {
			node: &yaml.Node{Kind: yaml.ScalarNode, Line: 12, Column: 7},
			want: ir.Provenance{Source: 4, Position: ir.Position{Line: 12, Column: 7}},
		},
		"a node the parser never placed carries no position": {
			node: &yaml.Node{},
			want: ir.Provenance{Source: 4},
		},
		"no node names the document alone": {
			node: nil,
			want: ir.Provenance{Source: 4},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tree.Node(tt.node))
		})
	}
}

// TestTree_NodeTakesTheOverlaysPlaceOverTheTrees pins the order Node asks in:
// a node the overlay grafted is placed where the overlay put it, with no line
// and column of its own and whatever pointer the tree gives it (GitHub #476);
// any other node is placed in the document, at the pointer the tree gives.
func TestTree_NodeTakesTheOverlaysPlaceOverTheTrees(t *testing.T) {
	t.Parallel()
	grafted := &yaml.Node{Kind: yaml.ScalarNode}
	own := &yaml.Node{Kind: yaml.ScalarNode, Line: 3, Column: 9}
	tree := provenance.Tree{
		Doc: provenance.Doc{Source: 0, Overlay: provenance.Attributed(1, nil,
			map[*yaml.Node]jsontext.Pointer{grafted: "/info/title"})},
		Pointers: func(*yaml.Node) (jsontext.Pointer, bool) { return "/from/the/tree", true },
	}

	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/title"}, tree.Node(grafted))
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/from/the/tree", Position: ir.Position{Line: 3, Column: 9}},
		tree.Node(own))
	assert.Equal(t, ir.Provenance{Source: 0}, tree.Node(nil), "the tree is not asked about no node")
}
