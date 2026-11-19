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

// dynamicRefValue is the $dynamicRef the schema node of document id writes, read
// as refOf reads a $ref, and whether it writes a string one.
func dynamicRefValue(docsValue *docs, id DocID, node *yaml.Node) (string, bool) {
	if node = nodeview.Deref(node); node == nil || node.Kind != yaml.MappingNode {
		return "", false
	}
	nodeValue := nodeview.Deref(docsValue.list[id].view.ChildByToken(node, "$dynamicRef"))
	if nodeValue == nil || nodeValue.Kind != yaml.ScalarNode || nodeValue.Tag == "!!null" {
		return "", false
	}
	return nodeValue.Value, true
}

// namesDynamic reports whether the $dynamicRef value, written at documentPointer and reached
// along path, is dynamic: its fragment is a plain name and it resolves, as a
// $ref, onto a $dynamicAnchor of that name. It is not shown to be the $ref it
// spells where the resource it names declares the name more than once, one
// of the declarations a $dynamicAnchor (duplicateDynamic), nor where a merge
// past the bound or the budget may hide the anchor: the lowering keeps those
// (docs/compilers/openapi.md#dynamic-ref). known is false, and nothing kept,
// while its hop waits for a document not yet read.
func namesDynamic(b *binder, documentPointer DocumentPointer, path []*yaml.Node, value string) (dynamic, known bool) {
	if v, ok := b.dyn.named[documentPointer]; ok {
		return v, true
	}
	name := string(fragment.Pointer(value))
	if name == "" || strings.HasPrefix(name, "/") {
		b.dyn.named[documentPointer] = false
		return false, true
	}
	hop := b.resolve(documentPointer, path, value, true)
	switch {
	case hop.Reason == PendingExternalDocumentRead:
		return false, false
	case b.work.over():
		return true, true
	case hop.Status == Resolved:
		dynamic = slices.Contains(b.ids.dynamic[dynamicKey{doc: hop.Target.Doc, name: name}], hop.Target)
	case hop.Reason == DuplicateAnchor:
		dynamic = duplicateDynamic(b, documentPointer, path, value, name)
	default:
		dynamic = hop.Reason == MergeBound
	}
	b.dyn.named[documentPointer] = dynamic
	return dynamic, true
}

// duplicateDynamic reports whether one of the declarations of the plain name
// name that the resource the $dynamicRef value, written at documentPointer and reached
// along path, names holds two or more of is a $dynamicAnchor: the resource's
// own, as resolve read them, and no other resource's of its document. A
// resource it cannot name again, which resolve named, reads as dynamic.
func duplicateDynamic(b *binder, documentPointer DocumentPointer, path []*yaml.Node, value, name string) bool {
	in, known := b.resources.holding(documentPointer, path, true, b.ids.readsDialects(documentPointer.Doc))
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
	for _, dynamicSite := range b.find.freshDynamic {
		if b.work.cancelled() {
			break
		}
		if dynamicSite.beside {
			dynamic = append(dynamic, dynamicSite)
			continue
		}
		path, _ := b.docs.path(dynamicSite.at)
		named, known := namesDynamic(b, dynamicSite.at, path, dynamicSite.value)
		if named {
			dynamic = append(dynamic, dynamicSite)
			continue
		}
		if !known {
			b.dyn.waiting[dynamicSite.at] = dynamicSite.value
		}
		if !b.find.sites[dynamicSite.at] {
			b.find.sites[dynamicSite.at] = true
			b.find.fresh = append(b.find.fresh, dynamicSite.at)
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

// keywordAt is the keyword the reference whose hop the pass read at documentPointer is
// written under.
func keywordAt(b *binder, documentPointer DocumentPointer) string {
	if b.dyn.spelled[documentPointer] {
		return "$dynamicRef"
	}
	return "$ref"
}
