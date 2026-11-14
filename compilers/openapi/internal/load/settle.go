// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// settle makes the tree read as the table judged it. Each value the table
// refused as the wrong type where it is written, and accepted nowhere another
// place reads it, is read as the field left out by every reader of the source,
// the model among them (docs/compilers/openapi.md#validation), for good; a
// verbatim copy still renders what the source wrote (nodeview.Written). Then
// the shapes the model faults on are set aside in a until the model is built
// (docs/parser-library.md#nil-populate-panic,
// docs/compilers/openapi.md#references): a Reference Object whose $ref
// is null names nothing, and a Path Item's ill-typed $ref and an
// additionalOperations entry that is no mapping are absent.
func settle(j judgment, a *aside) {
	for n := range j.refused {
		if !j.accepted[n] {
			nodeview.Unread(n)
		}
	}
	for _, ref := range j.referenced {
		if v := nodeview.Deref(nodeview.Child(ref, "$ref")); v != nil && isNullNode(v) {
			a.spoil(v)
		}
	}
	for key := range j.seen {
		if key.kind == oas.PathItem {
			asidePathItem(key.node, a)
		}
	}
}

// asidePathItem sets aside, in the path item mapping item, its own $ref when
// that is a null or no scalar, the item's own field the table reads as left
// out, and each additionalOperations entry that is no mapping, in that
// mapping and in each a `<<` of it merges.
func asidePathItem(item *yaml.Node, a *aside) {
	a.hide(item, func(key, val *yaml.Node) bool {
		name, own := nodeview.KeyOf(key)
		v := nodeview.Deref(val)
		return own && name == "$ref" && (v == nil || v.Kind != yaml.ScalarNode || isNullNode(v))
	})
	ops := nodeview.Deref(nodeview.Child(item, "additionalOperations"))
	if ops == nil || ops.Kind != yaml.MappingNode {
		return
	}
	noOperation := func(key, val *yaml.Node) bool { return !nodeview.IsMergeKey(key) && nodeview.NotMapping(val) }
	a.hide(ops, noOperation)
	for i := 0; i+1 < len(ops.Content); i += 2 {
		if nodeview.IsMergeKey(ops.Content[i]) {
			for _, src := range nodeview.MergeSources(ops.Content[i+1]) {
				a.hide(nodeview.Deref(src), noOperation)
			}
		}
	}
}

// isNullNode reports whether n is a null scalar.
func isNullNode(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}
