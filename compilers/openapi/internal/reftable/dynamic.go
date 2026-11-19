// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// dynamicRefs is what a pass reads of the $dynamicRefs schemas write beside no
// $ref, which is that schema's entry where it writes one. JSON Schema 2020-12 §8.2.3.2 resolves one as a $ref, and
// reads it as dynamic only where that lands on a fragment a $dynamicAnchor
// created: named holds each verdict known so far, true for such a one, and
// spelled each position whose hop read one (ReferenceSite.Keyword). waiting is each
// site settled as a $ref while its verdict waits for a document (resettle).
type dynamicRefs struct {
	named   map[DocumentPointer]bool
	spelled map[DocumentPointer]bool
	waiting map[DocumentPointer]string
}

// newDynamicRefs returns a pass's reading of $dynamicRefs, holding none.
func newDynamicRefs() *dynamicRefs {
	return &dynamicRefs{named: map[DocumentPointer]bool{}, spelled: map[DocumentPointer]bool{}, waiting: map[DocumentPointer]string{}}
}

// dynamicRefValue is the $dynamicRef the schema n of document id writes, read
// as refOf reads a $ref, and whether it writes a string one.
func dynamicRefValue(d *docs, id DocID, n *yaml.Node) (string, bool) {
	if n = nodeview.Deref(n); n == nil || n.Kind != yaml.MappingNode {
		return "", false
	}
	v := nodeview.Deref(d.list[id].view.ChildByToken(n, "$dynamicRef"))
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
		return "", false
	}
	return v.Value, true
}

// namesDynamic reports whether the $dynamicRef value, written at p and reached
// along path, is dynamic: its fragment is a plain name and it resolves, as a
// $ref, onto a $dynamicAnchor of that name. It is not shown to be the $ref it
// spells where the resource it names declares the name more than once, one
// of the declarations a $dynamicAnchor (duplicateDynamic), nor where a merge
// past the bound or the budget may hide the anchor: the lowering keeps those
// (docs/compilers/openapi.md#dynamic-ref). known is false, and nothing kept,
// while its hop waits for a document not yet read.
func namesDynamic(b *binder, p DocumentPointer, path []*yaml.Node, value string) (dynamic, known bool) {
	if v, ok := b.dyn.named[p]; ok {
		return v, true
	}
	name := string(fragment.Pointer(value))
	if name == "" || strings.HasPrefix(name, "/") {
		b.dyn.named[p] = false
		return false, true
	}
	h := b.resolve(p, path, value, true)
	switch {
	case h.Reason == PendingExternalDocumentRead:
		return false, false
	case b.work.over():
		return true, true
	case h.Status == Resolved:
		dynamic = slices.Contains(b.ids.dynamic[dynamicKey{doc: h.Target.Doc, name: name}], h.Target)
	case h.Reason == DuplicateAnchor:
		dynamic = duplicateDynamic(b, p, path, value, name)
	default:
		dynamic = h.Reason == MergeBound
	}
	b.dyn.named[p] = dynamic
	return dynamic, true
}

// duplicateDynamic reports whether one of the declarations of the plain name
// name that the resource the $dynamicRef value, written at p and reached
// along path, names holds two or more of is a $dynamicAnchor: the resource's
// own, as resolve read them, and no other resource's of its document. A
// resource it cannot name again, which resolve named, reads as dynamic.
func duplicateDynamic(b *binder, p DocumentPointer, path []*yaml.Node, value, name string) bool {
	in, known := b.resources.holding(p, path, true, b.ids.readsDialects(p.Doc))
	uri, _, _ := strings.Cut(value, "#")
	to, reason, _ := b.named(in, uri)
	dynamic := b.ids.dynamic[dynamicKey{doc: to.root.Doc, name: name}]
	return !known || reason != NoReason || slices.ContainsFunc(b.ids.anchors[anchorKey{root: to.root, name: name}],
		func(at DocumentPointer) bool { return slices.Contains(dynamic, at) })
}

// staticSites moves each $dynamicRef noted since the table last settled that
// is the $ref it spells, or may be once the document its hop waits for is
// read, among the sites to settle, as a $ref written there would be. The
// dynamic ones, and those beside a $ref, are left to land (landDynamic). A
// binding its context stopped reads none further.
func staticSites(b *binder) {
	dynamic := b.find.freshDynamic[:0]
	for _, d := range b.find.freshDynamic {
		if b.work.cancelled() {
			break
		}
		if d.beside {
			dynamic = append(dynamic, d)
			continue
		}
		path, _ := b.docs.path(d.at)
		named, known := namesDynamic(b, d.at, path, d.value)
		if named {
			dynamic = append(dynamic, d)
			continue
		}
		if !known {
			b.dyn.waiting[d.at] = d.value
		}
		if !b.find.sites[d.at] {
			b.find.sites[d.at] = true
			b.find.fresh = append(b.find.fresh, d.at)
		}
	}
	b.find.freshDynamic = dynamic
}

// resettle reports whether site, settled as a $ref while its $dynamicRef's
// verdict waited for a document, is dynamic now that verdict is known, and
// leaves it to land (landDynamic) where it is: it has no entry.
func resettle(b *binder, site DocumentPointer) bool {
	value, waited := b.dyn.waiting[site]
	if !waited {
		return false
	}
	path, _ := b.docs.path(site)
	named, known := namesDynamic(b, site, path, value)
	if known {
		delete(b.dyn.waiting, site)
	}
	if named {
		b.find.freshDynamic = append(b.find.freshDynamic, dynamicSite{at: site, value: value})
	}
	return named
}

// keywordAt is the keyword the reference whose hop the pass read at p is
// written under.
func keywordAt(b *binder, p DocumentPointer) string {
	if b.dyn.spelled[p] {
		return "$dynamicRef"
	}
	return "$ref"
}
