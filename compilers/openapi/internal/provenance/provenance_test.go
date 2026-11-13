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

// TestAttribution_WriterOfNamesTheOverlayWhenItWroteEveryPosition pins the rule
// for a construct no single position addresses: the overlay is its writer when
// it supplied every position the construct combines, else the answer for the
// position it is recorded at, and 0, the document writing it, otherwise. The
// overlay's index is not 1, so an answer of a constant index could not pass.
func TestAttribution_WriterOfNamesTheOverlayWhenItWroteEveryPosition(t *testing.T) {
	t.Parallel()
	const overlay, base = 5, 0
	attributed := provenance.Attributed(overlay, written("/s/if", "/s/then", "/t", "/t/if"))
	tests := []struct {
		name        string
		attribution provenance.Attribution
		pointer     jsontext.Pointer
		from        []jsontext.Pointer
		want        int
	}{
		{name: "no positions is the pointer's own answer", attribution: attributed, pointer: "/t", want: overlay},
		{name: "no positions, base-owned pointer", attribution: attributed, pointer: "/s", want: base},
		{name: "every position the overlay's", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/if", "/s/then"}, want: overlay},
		{name: "positions from both documents", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/if", "/s/else"}, want: base},
		{name: "positions from both documents, the overlay's last", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/else", "/s/if"}, want: base},
		{name: "every position the base's", attribution: attributed, pointer: "/s", from: []jsontext.Pointer{"/s/else"}, want: base},
		{name: "an overlay-introduced declaring position", attribution: attributed, pointer: "/t", from: []jsontext.Pointer{"/t/if"}, want: overlay},
		{name: "no overlay applied", pointer: "/s", from: []jsontext.Pointer{"/s/if"}, want: base},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.attribution.WriterOf(tc.pointer, tc.from))
		})
	}
}

// TestDoc_AtPlacesAPointerInItsDocumentAndNamesTheWriter pins Doc.At over the
// attribution it carries: every pointer is placed in the document, one the
// overlay supplied names the overlay as its writer, and a record assembled from
// several positions names it only where it supplied all of them.
func TestDoc_AtPlacesAPointerInItsDocumentAndNamesTheWriter(t *testing.T) {
	t.Parallel()
	doc := provenance.Doc{Source: 4, Overlay: provenance.Attributed(5, written("/a", "/s/if", "/s/then"))}

	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/a", WrittenBy: 5}, doc.At("/a"))
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/b"}, doc.At("/b"))
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/s", WrittenBy: 5}, doc.At("/s", "/s/if", "/s/then"))
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
// a node the overlay grafted is placed in the document where the overlay put
// it, the overlay its writer, with no line and column of its own and whatever
// pointer the tree gives it (GitHub #476); any other node is placed in the
// document, at the pointer the tree gives.
func TestTree_NodeTakesTheOverlaysPlaceOverTheTrees(t *testing.T) {
	t.Parallel()
	grafted := &yaml.Node{Kind: yaml.ScalarNode}
	own := &yaml.Node{Kind: yaml.ScalarNode, Line: 3, Column: 9}
	positions := provenance.NewPositions()
	positions.Place(grafted, at(positions, "/info/title"))
	tree := provenance.Tree{
		Doc:      provenance.Doc{Source: 2, Overlay: provenance.Attributed(1, positions)},
		Pointers: func(*yaml.Node) (jsontext.Pointer, bool) { return "/from/the/tree", true },
	}

	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/info/title", WrittenBy: 1}, tree.Node(grafted))
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/from/the/tree", Position: ir.Position{Line: 3, Column: 9}},
		tree.Node(own))
	assert.Equal(t, ir.Provenance{Source: 2}, tree.Node(nil), "the tree is not asked about no node")
}

// at returns the position pointer names in positions, recording it and every
// position above it.
func at(positions *provenance.Positions, pointer jsontext.Pointer) *provenance.Position {
	p := positions.Root()
	for token := range pointer.Tokens() {
		p = positions.Child(p, token)
	}
	return p
}

// written returns a record of the positions pointers name, each written.
func written(pointers ...jsontext.Pointer) *provenance.Positions {
	positions := provenance.NewPositions()
	for _, p := range pointers {
		at(positions, p).Write()
	}
	return positions
}

// TestAttribution_WriterAtReadsAPointerTokenByToken pins how WriterAt reads
// the record: a position the overlay wrote answers its index, one recorded
// only on the way to one, a position beneath one, the root and a position
// never recorded answer 0, a token holding "/" or "~" is read unescaped, and a
// nil record, as an Attribution's zero value holds, answers 0 for any.
func TestAttribution_WriterAtReadsAPointerTokenByToken(t *testing.T) {
	t.Parallel()
	a := provenance.Attributed(3, written("/paths/~1a~0b/get", "/tags/0"))

	tests := map[jsontext.Pointer]int{
		"/paths/~1a~0b/get":         3,
		"/tags/0":                   3,
		"/paths/~1a~0b":             0,
		"/paths/~1a~0b/get/summary": 0,
		"":                          0,
		"/tags/1":                   0,
		"/paths/~1a~0c/get":         0,
	}
	for pointer, want := range tests {
		assert.Equal(t, want, a.WriterAt(pointer), "%q", pointer)
		assert.Equal(t, 0, provenance.Attributed(3, nil).WriterAt(pointer), "%q", pointer)
	}
}

// TestAttribution_AtSpellsThePointerOfAPlacedNode pins the node half: a node
// placed at a position answers that position's pointer, escaped as RFC 6901
// writes it, the root's included, and any other node answers none.
func TestAttribution_AtSpellsThePointerOfAPlacedNode(t *testing.T) {
	t.Parallel()
	positions := provenance.NewPositions()
	deep, root, other := &yaml.Node{}, &yaml.Node{}, &yaml.Node{}
	positions.Place(deep, at(positions, "/paths/~1a~0b/get"))
	positions.Place(root, positions.Root())
	a := provenance.Attributed(1, positions)

	got, found := a.At(deep)
	assert.True(t, found)
	assert.Equal(t, jsontext.Pointer("/paths/~1a~0b/get"), got)
	got, found = a.At(root)
	assert.True(t, found)
	assert.Equal(t, jsontext.Pointer(""), got)
	_, found = a.At(other)
	assert.False(t, found)
	_, found = provenance.Attributed(1, nil).At(deep)
	assert.False(t, found, "a nil record places nothing")
}

// TestPositions_ChildRecordsAPositionOnce pins that asking for a position
// twice returns the one recorded, so a write through either is the same
// write.
func TestPositions_ChildRecordsAPositionOnce(t *testing.T) {
	t.Parallel()
	positions := provenance.NewPositions()
	first := positions.Child(positions.Root(), "a")
	assert.Same(t, first, positions.Child(positions.Root(), "a"))
	assert.NotSame(t, first, positions.Child(positions.Root(), "b"))
	assert.NotSame(t, first, positions.Child(first, "a"), "a token names a position within its holder alone")
}
