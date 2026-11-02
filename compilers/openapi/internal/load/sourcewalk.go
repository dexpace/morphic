// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
)

// walkSource returns the walk of doc with the fields beside each path item $ref
// interleaved (Siblings.interleaved), followed by each components/mediaTypes
// entry m models, at its pointer, and each content entry written as a $ref
// met on the way (within), so every pass meets them in the order the source
// writes them. A document with neither, which is nearly every one, is walked
// as the library walks it.
func walkSource(ctx context.Context, doc *soa.OpenAPI, s Siblings, m MediaTypes) iter.Seq[soa.WalkItem] {
	items := walk(ctx, doc)
	if len(s.list) > 0 {
		items = s.interleaved(ctx, items)
	}
	if len(m.declared) > 0 {
		items = m.followedByDeclared(ctx, items)
	}
	return m.within(ctx, items)
}

// interleaved yields items and, after each $ref whose fields were modelled where
// the walk meets it, that model's walk, interleaved alike. The open walks are a
// stack rather than a recursion, and each model is entered once, at the one
// position it was found at, so the stack holds at most one walk per model
// beyond the walk of items itself.
func (s Siblings) interleaved(ctx context.Context, items iter.Seq[soa.WalkItem]) iter.Seq[soa.WalkItem] {
	return func(yield func(soa.WalkItem) bool) {
		var open []func() (soa.WalkItem, bool)
		var stops []func()
		defer func() {
			for _, stop := range stops {
				stop()
			}
		}()
		push := func(seq iter.Seq[soa.WalkItem]) {
			next, stop := iter.Pull(seq)
			open, stops = append(open, next), append(stops, stop)
		}
		push(items)
		for len(open) > 0 {
			item, ok := open[len(open)-1]()
			if !ok {
				open = open[:len(open)-1]
				continue
			}
			if !yield(item) {
				return
			}
			if i, entered := s.entry(item); entered {
				push(s.modelWalk(ctx, i))
			}
		}
	}
}

// entry returns the model of the fields beside the $ref item is, when item is
// met at the position that model was found at.
func (s Siblings) entry(item soa.WalkItem) (int, bool) {
	at, entered := 0, false
	// Match returns only what its matcher does, and this one never fails.
	_ = item.Match(soa.Matcher{ReferencedPathItem: func(rp *soa.ReferencedPathItem) error {
		i, ok := s.byNode[useSite(rp)]
		entered = ok && s.list[i].Pointer == jsontext.Pointer(item.Location.ToJSONPointer())
		at = i
		return nil
	}})
	return at, entered
}

// modelWalk returns the walk of the i-th model, located where its use site
// writes it, less the wrapper the walk starts from, which the source does not
// write. A model found inside it is not entered: siblingsFrom queues each in
// turn.
func (s Siblings) modelWalk(ctx context.Context, i int) iter.Seq[soa.WalkItem] {
	start := &soa.ReferencedPathItem{Object: s.list[i].Item}
	return locatedWalk(func(yield func(soa.WalkItem) bool) {
		for item := range walk(ctx, start) {
			if len(item.Location) > 0 && !yield(item) {
				return
			}
		}
	}, s.list[i].Pointer)
}

// locatedWalk returns items, a walk started from an object other than the
// document, with each location placed under pointer, where the source writes
// that object. Rendered alone, such a location is wrong wherever it starts at
// a map key, such as a path item's method: it renders "//get" for "/get".
func locatedWalk(items iter.Seq[soa.WalkItem], pointer jsontext.Pointer) iter.Seq[soa.WalkItem] {
	prefix := locationOf(pointer)
	return func(yield func(soa.WalkItem) bool) {
		for item := range items {
			item.Location = append(slices.Clip(prefix), item.Location...)
			if !yield(item) {
				return
			}
		}
	}
}

// locationOf returns the walk location that renders as pointer. The first token
// is a field and each later one a key, because a location renders its first
// field without a separator and every key with one. The first token is never
// empty, which a field would drop: a use site's names a field of the OpenAPI
// Object, and a reached object's a top-level key, which the resolver never
// reads as empty: it takes "#/" for the document, as navigation's
// TestTokens_DecodesAsTheLibraryReads holds it to.
func locationOf(pointer jsontext.Pointer) soa.Locations {
	var loc soa.Locations
	for token := range pointer.Tokens() {
		if len(loc) == 0 {
			loc = append(loc, soa.LocationContext{ParentField: token})
			continue
		}
		loc = append(loc, soa.LocationContext{ParentKey: &token})
	}
	return loc
}
