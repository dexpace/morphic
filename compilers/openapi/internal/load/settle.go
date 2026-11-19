// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// settle makes the tree read as the table judged it, once every judgment is
// in: each value the table refused and no place accepted is turned
// (turnRefused), and then the shapes the model faults on are set aside
// (setAside). A document whose Schema Objects wait for their dialect
// (judgeSchemas) is not judged whole yet, so its values are turned once they
// are, before any landing is built (loading.bindRefs).
func settle(judgmentValue judgment, a *aside) {
	if len(judgmentValue.pending) == 0 {
		turnRefused(judgmentValue)
	}
	setAside(judgmentValue, a)
}

// turnRefused reads each value the table refused as the wrong type, and
// accepted nowhere another place reads it, as the field left out by every
// reader, for good (docs/compilers/openapi.md#validation); a verbatim copy
// renders what the source wrote (nodeview.Written). It reports whether it
// turned any.
func turnRefused(judgmentValue judgment) bool {
	turned := false
	for node := range judgmentValue.refused {
		if !judgmentValue.accepted[node] {
			nodeview.Unread(node)
			turned = true
		}
	}
	return turned
}

// setAside sets aside in a, until the model is built, the shapes the model
// faults on (docs/parser-library.md#nil-populate-panic,
// docs/compilers/openapi.md#references): a Reference Object whose $ref is null
// names nothing, and a Path Item's ill-typed $ref and an additionalOperations
// entry that is no mapping are absent. A second call sets aside what a later
// turn left so.
func setAside(judgmentValue judgment, a *aside) {
	for _, ref := range judgmentValue.referenced {
		node := nodeview.Child(ref, "$ref")
		switch {
		case node == nil || !isNullNode(node):
		case writesRef(ref):
			a.spoil(node)
		default:
			// The model reads a Reference Object by the keys the mapping writes
			// itself, so one whose $ref a `<<` merges in stands in its place.
			a.standIn(ref)
		}
	}
	for key := range judgmentValue.seen {
		if key.kind == specrules.PathItem {
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
		node := nodeview.Deref(val)
		return own && name == "$ref" && (node == nil || node.Kind != yaml.ScalarNode || isNullNode(node))
	})
	operations := nodeview.Child(item, "additionalOperations")
	if operations == nil || operations.Kind != yaml.MappingNode {
		return
	}
	noOperation := func(key, val *yaml.Node) bool { return !nodeview.IsMergeKey(key) && nodeview.NotMapping(val) }
	a.hide(operations, noOperation)
	for i := 0; i+1 < len(operations.Content); i += 2 {
		if nodeview.IsMergeKey(operations.Content[i]) {
			for _, src := range nodeview.MergeSources(operations.Content[i+1]) {
				a.hide(nodeview.Deref(src), noOperation)
			}
		}
	}
}

// isNullNode reports whether node is a null scalar.
func isNullNode(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null"
}

// writesRef reports whether the mapping node writes the key $ref itself, rather
// than through a `<<` merge.
func writesRef(node *yaml.Node) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if key, own := nodeview.KeyOf(node.Content[i]); own && key == "$ref" {
			return true
		}
	}
	return false
}
