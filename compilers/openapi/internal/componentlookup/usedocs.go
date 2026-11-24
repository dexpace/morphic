// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package componentlookup

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
	"github.com/dexpace/morphic/ir"
)

// UseDocs folds the summary and description written beside each `$ref` of ref's
// chain over docs, the declaration's, from it outward, field by field, so the
// use site's own win, an alias's beat what it names, and a field a hop omits,
// or writes empty or null, leaves the one beneath standing (GitHub #610). Each
// is read off the mapping writing the $ref, as the model reads it, a `<<`
// included. OpenAPI gives such a sibling "no effect" where the object allows
// none, but the IR has a docs field at each position this serves
// (docs/compilers/openapi.md#reference-object-siblings).
func UseDocs[T, S any, R interface {
	*S
	Referenced[T]
}](b *typed.Bindings, ref R, docs ir.Docs) ir.Docs {
	if _, ok := b.Reference(ref); !ok {
		return docs
	}
	written := []*yaml.Node{writtenAs(ref)}
	for _, hop := range Hops[T, S](b, ref) {
		written = append(written, writtenAs(hop.Ref))
	}
	for _, node := range slices.Backward(written) {
		if summary, ok := DocText(node, "summary"); ok {
			docs.Summary = summary
		}
		if description, ok := DocText(node, "description"); ok {
			docs.Description = description
		}
	}
	return docs
}

// WrittenAt returns the mapping the object b holds at at, read as kind, was
// read from, or nil where b holds none there.
func WrittenAt(b *typed.Bindings, at reftable.DocumentPointer, kind reftable.RefKind) *yaml.Node {
	return writtenAs(b.ObjectAt(at, kind))
}

// writtenAs returns the mapping the wrapper model was read from, which its
// core holds, or nil for one with none.
func writtenAs(model any) *yaml.Node {
	holder, ok := model.(interface{ GetCoreAny() any })
	if !ok {
		return nil
	}
	core, ok := holder.GetCoreAny().(interface{ GetRootNode() *yaml.Node })
	if !ok {
		return nil
	}
	return core.GetRootNode()
}

// DocText returns the text the mapping node writes under key, and false where
// it writes none: no such key, or a value that is null, empty or no scalar.
func DocText(node *yaml.Node, key string) (string, bool) {
	child := nodeview.Deref(nodeview.Child(node, key))
	if child == nil || child.Kind != yaml.ScalarNode || child.Value == "" || child.ShortTag() == "!!null" {
		return "", false
	}
	return child.Value, true
}
