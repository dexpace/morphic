// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"slices"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// refKey is the key that makes a Path Item Object a reference.
const refKey = "$ref"

// Siblings holds what the source writes beside its path items' $refs, each use
// site's fields modelled as a Path Item of their own. The library's reference
// wrapper keeps none of them: its Object is nil on the $ref branch, so the
// operations, parameters, servers, x-* and undeclared keys beside a $ref reach
// no model (GitHub #577). Every pass of this phase reads them as part of the
// source, so their references are indexed where the binder reads them, and
// their findings are reported, as the document's own are. The zero value holds
// none.
type Siblings struct {
	// byNode is each modelled use site's index in list, by the mapping it is
	// written as, and faulted each use site the parser faulted on, whose fault
	// is reported once.
	byNode  map[*yaml.Node]int
	faulted map[*yaml.Node]bool
	list    []Sibling
}

// Sibling is the fields one use site writes beside its $ref: the Path Item they
// model, and the pointer of the first position the walk met them at.
type Sibling struct {
	Item    *soa.PathItem
	Pointer jsontext.Pointer
}

// Of returns the fields written beside rp's $ref, and false when rp is no
// reference or writes nothing beside one.
//
// It answers by the mapping the use site is written as, not by rp, so a
// reference the resolver reached through a chain finds the fields its own
// wrapper was built from, and an alias reaching one mapping twice finds one
// model.
func (s Siblings) Of(rp *soa.ReferencedPathItem) (Sibling, bool) {
	if !isReference(useSite(rp)) {
		return Sibling{}, false
	}
	i, ok := s.byNode[useSite(rp)]
	if !ok {
		return Sibling{}, false
	}
	return s.list[i], true
}

// Unread reports whether rp writes fields beside its $ref that no model holds:
// a reference this phase's walk does not reach, in another document or at a
// position of the source no object it walks holds, such as under an x-* key.
// One the parser faulted on was reported where it is written, so it is not.
func (s Siblings) Unread(rp *soa.ReferencedPathItem) bool {
	node := useSite(rp)
	if !isReference(node) {
		return false
	}
	if _, met := s.byNode[node]; met || s.faulted[node] {
		return false
	}
	return writesBeside(node)
}

// useSite returns the mapping rp is written as, through any alias, or nil for
// a nil rp and one with no node.
func useSite(rp *soa.ReferencedPathItem) *yaml.Node {
	if rp == nil {
		return nil
	}
	return nodeview.Deref(rp.GetCore().GetRootNode())
}

// siblingBuild models the fields of one use site, or reports why it could not.
type siblingBuild func(ctx context.Context, node *yaml.Node) (*soa.PathItem, error)

// modelSiblings models the fields beside every path item $ref the walk of doc
// reaches, and beside every one inside what it models, returning an error
// diagnostic for each use site the parser faulted on.
//
// A tree in which no mapping keys $ref beside another key holds no use site and
// is not walked: that is nearly every document, and the walk is a pass over the
// whole model.
func modelSiblings(ctx context.Context, doc *soa.OpenAPI, tree provenance.Tree) (Siblings, []ir.Diagnostic) {
	if !anyNode(ctx, doc.GetRootNode(), refBeside) {
		return Siblings{}, nil
	}
	return siblingsFrom(ctx, walk(ctx, doc), tree, buildSibling)
}

// refBeside reports whether n is a reference writing a field beside its $ref,
// as every use site holding fields beside its $ref is (see withoutRef).
func refBeside(n *yaml.Node) bool {
	return isReference(n) && writesBeside(n)
}

// isReference reports whether node is a reference as the bindings' table reads
// one, which the lowering mounts through: a mapping whose $ref, read through
// its merges, holds a string (docs/compilers/openapi.md#references). The
// parser's wrapper reads its own pairs alone, so a $ref a `<<` merges in makes
// no reference to it.
func isReference(node *yaml.Node) bool {
	_, writes := writtenRef(node)
	return writes && !unreadableRef(node)
}

// siblingsFrom is modelSiblings over a walk and a model build the caller
// supplies, so a test can hand it a walk that panics, or a build that faults.
//
// The doc's walk comes first, then each model's in the order it was found, so
// a model found inside another is walked too. Each mapping is modelled once,
// so there are no more models than mappings the source holds, which its node
// bound caps. A walk that panics stops the search where it stands; the
// resolution walk meets the same fault at the same model, and reports it.
func siblingsFrom(ctx context.Context, items iter.Seq[soa.WalkItem], tree provenance.Tree, build siblingBuild,
) (Siblings, []ir.Diagnostic) {
	s := Siblings{byNode: map[*yaml.Node]int{}, faulted: map[*yaml.Node]bool{}}
	var faults []ir.Diagnostic
	met := func(site jsontext.Pointer, rp *soa.ReferencedPathItem) error {
		faults = append(faults, s.add(ctx, site, rp, build, tree)...)
		return nil
	}
	_, err := eachModel(items, "sibling search", met)
	// The document's walk, then each model's in turn, until one panics or ctx
	// is done.
	for i := 0; err == nil && i < len(s.list) && i < sourceindex.MaxIndexedNodes && ctx.Err() == nil; i++ {
		_, err = eachModel(s.modelWalk(ctx, i), "sibling search", met)
	}
	return s, faults
}

// add models the fields rp's use site writes beside its $ref, at site. It does
// nothing for an rp that is no reference, nor for a mapping holding nothing
// beside the $ref, nor for a mapping already met. A mapping the parser faulted
// on holds no model, and its fault is returned as an error diagnostic instead.
func (s *Siblings) add(ctx context.Context, site jsontext.Pointer, rp *soa.ReferencedPathItem, build siblingBuild,
	tree provenance.Tree,
) []ir.Diagnostic {
	node := useSite(rp)
	if !isReference(node) || s.faulted[node] {
		return nil
	}
	if _, seen := s.byNode[node]; seen {
		return nil
	}
	fields, ok := withoutRef(node)
	if !ok {
		return nil
	}
	item, err := build(ctx, fields)
	if err != nil {
		s.faulted[node] = true
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, tree.Node(node),
			"the fields written beside this path item's $ref could not be read: %s", err.Error())}
	}
	s.byNode[node] = len(s.list)
	s.list = append(s.list, Sibling{Item: item, Pointer: site})
	return nil
}

// withoutRef returns a copy of the mapping node less every pair keyed $ref, its
// own and those a `<<` merges in, which it copies the same way, and false when
// node is no mapping or that leaves nothing (writesBeside). The copy keeps
// node's position and the document's own key and value nodes, so every finding
// and raw read lands where the source writes it.
func withoutRef(node *yaml.Node) (*yaml.Node, bool) {
	if !writesBeside(node) {
		return nil, false
	}
	return withoutRefIn(node, 0), true
}

// withoutRefIn is withoutRef's copy of the mapping node, depth `<<` merges
// below the use site, each source copied to MergeDepthLimit, as far as the
// decoder of the copy reads one (docs/parser-library.md#merge-keys).
func withoutRefIn(node *yaml.Node, depth int) *yaml.Node {
	fields := *node
	fields.Content = make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		switch {
		case isRefKey(key):
			continue
		case nodeview.IsMergeKey(key) && depth < nodeview.MergeDepthLimit:
			val = mergedWithoutRef(val, depth+1)
		}
		fields.Content = append(fields.Content, key, val)
	}
	return &fields
}

// mergedWithoutRef is the `<<` value val, a mapping or a list of them, each
// copied less its $ref (withoutRefIn), and val itself for anything else, which
// merges nothing.
func mergedWithoutRef(val *yaml.Node, depth int) *yaml.Node {
	v := nodeview.Deref(val)
	if v.Kind == yaml.MappingNode {
		return withoutRefIn(v, depth)
	}
	if v.Kind != yaml.SequenceNode {
		return val
	}
	list := *v
	list.Content = make([]*yaml.Node, 0, len(v.Content))
	for _, item := range v.Content {
		if m := nodeview.Deref(item); m != nil && m.Kind == yaml.MappingNode {
			item = withoutRefIn(m, depth)
		}
		list.Content = append(list.Content, item)
	}
	return &list
}

// writesBeside reports whether the mapping node holds a field other than its
// $ref, as the decoder reads the mapping, merges included. One merging nothing
// is answered off its own keys, which costs nothing like a merge's reading.
func writesBeside(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	merges := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		switch {
		case nodeview.IsMergeKey(node.Content[i]):
			merges = true
		case !isRefKey(node.Content[i]):
			return true
		}
	}
	return merges && slices.ContainsFunc(nodeview.Pairs(node), func(p nodeview.Pair) bool { return p.Key != refKey })
}

// isRefKey reports whether key spells $ref, directly or through an alias, as
// the parser reads a mapping's key (nodeview.KeyOf).
func isRefKey(key *yaml.Node) bool {
	k, own := nodeview.KeyOf(key)
	return own && k == refKey
}

// buildSibling builds the fields beside a $ref into a Path Item, as the source's
// own path items are built. Nothing is validated: the table judges them as the
// item's own fields.
func buildSibling(ctx context.Context, node *yaml.Node) (*soa.PathItem, error) {
	var item *soa.PathItem
	err := runRecovered("parser", func() error {
		item = soa.NewPathItem()
		_, err := marshaller.UnmarshalNode(ctx, "", node, item)
		return err
	})
	return item, err
}
