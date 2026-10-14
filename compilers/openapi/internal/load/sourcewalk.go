package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
)

// walkSource returns the walk of doc with the fields beside each path item $ref
// walked right after the $ref, so every pass meets them in the order the source
// writes them, as it meets an inline item's: which members of a failing chain
// are reported follows that order (GitHub #767). A document with none, which is
// nearly every one, is walked as the library walks it.
func walkSource(ctx context.Context, doc *soa.OpenAPI, s Siblings) iter.Seq[soa.WalkItem] {
	if len(s.list) == 0 {
		return soa.Walk(ctx, doc)
	}
	return s.interleaved(ctx, soa.Walk(ctx, doc))
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
	prefix := locationOf(s.list[i].Pointer)
	start := &soa.ReferencedPathItem{Object: s.list[i].Item}
	return func(yield func(soa.WalkItem) bool) {
		for item := range soa.Walk(ctx, start) {
			if len(item.Location) == 0 {
				continue
			}
			item.Location = append(slices.Clip(prefix), item.Location...)
			if !yield(item) {
				return
			}
		}
	}
}

// locationOf returns the walk location that renders as pointer. The first token
// is a field and each later one a key, because a location renders its first
// field without a separator and every key with one; a use site's first token
// is never empty, since it names a field of the OpenAPI Object.
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
