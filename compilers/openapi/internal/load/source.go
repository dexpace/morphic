// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"net/url"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
)

// sourceDocument is the source as its references' binding reads it: the path
// they are relative to (Location), the one LoadedOpenAPIDocument.Sources records, the bytes
// and the tree its model was built from, what the table found in it, and where
// a finding about a node of that tree is placed, at the $ref that reached it
// where the tree holds no node.
//
// A $ref from another document back into the source reads this tree, not the
// file, so it reaches what the compile holds: the tree an overlay patched, and
// nodes a finding is reported for once (GitHub #759).
type sourceDocument struct {
	path   string
	places *refscope.Places
	handed string
	data   []byte
	root   *yaml.Node
	judged judgment
	tree   provenance.Tree
	// mappings is where the caller put the documents under a URI prefix
	// (Options.Mappings).
	mappings []Mapping
}

// treeOf places the nodes of root in the document doc places, their pointers
// indexed under ctx once a node is first asked about (scan.Pointers), so the
// index stops where the load does.
func treeOf(ctx context.Context, doc provenance.Doc, root *yaml.Node) provenance.Tree {
	return provenance.Tree{Doc: doc, Pointers: scan.Pointers(ctx, root)}
}

// treeHolds reports whether tree holds node, so a finding about node is placed in
// tree's document. A tree with no pointers, such as the zero Tree, holds none.
func treeHolds(tree provenance.Tree, node *yaml.Node) bool {
	if tree.Pointers == nil {
		return false
	}
	_, held := tree.Pointers(node)
	return held
}

// newSourceDocument returns src, built from its data into root, which the
// table judged as judged, and whose findings tree places, the files the
// compile names lying in places.
func newSourceDocument(src compilers.Source, places *refscope.Places, root *yaml.Node, judged judgment,
	tree provenance.Tree,
) sourceDocument {
	return sourceDocument{path: Location(src), places: places, handed: src.Path, data: src.Data, root: root,
		judged: judged, tree: tree}
}

// fetchedFrom reports whether location is an absolute http(s) URL, naming a
// host, which a source fetched from the network is located at
// (compilers.Source): its references are then bounded as a fetched
// document's are.
func fetchedFrom(location string) bool {
	u, err := url.Parse(location)
	return err == nil && reftable.OverNetwork(location) && u.Host != ""
}

// Location is where src lies, as the references in it are read against: its
// Location, or its Path where the caller gave none (compilers.Source).
func Location(src compilers.Source) string {
	if src.Location != "" {
		return src.Location
	}
	return src.Path
}
