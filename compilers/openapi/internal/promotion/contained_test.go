// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// held is an entry of source src at pointer, holding the JSON value.
func held(src int, pointer, value string) ir.UnmodeledEntry {
	return ir.UnmodeledEntry{
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue(value),
		Provenance: ir.Provenance{Source: src, Pointer: jsontext.Pointer(pointer)},
	}
}

// noPromotions is the disabled policy, which reads no key, so it exempts none
// from a drop.
func noPromotions() promotion.Promoter {
	return promotion.New(promotion.ExtensionPromotions{Disabled: true})
}

// TestDropContained_DropsWhatAnotherEntryHoldsWhole holds the one shape the
// function exists for: an entry lying strictly beneath another's in the same
// source, whose value is what the other holds there, is dropped.
func TestDropContained_DropsWhatAnotherEntryHoldsWhole(t *testing.T) {
	t.Parallel()
	p := ir.Unmodeled{
		"openapi:s":    held(0, "/s", `{"if":{"x":1}}`),
		"openapi:s/if": held(0, "/s/if/x", `1`),
	}
	promotion.DropContained(noPromotions(), &p)
	assert.Equal(t, []string{"openapi:s"}, keysOf(p))
}

// TestDropContained_KeepsWhatIsNotInside holds each look-alike to its entry:
// beside an entry at /s holding {"if":{"x":1}}, one at /s/x (the value holds
// nothing at /x), one at /s/if/x holding another value, and one at /s/if/x in
// another source are all kept; and where whole names an entry other than the
// one holding it, nothing is dropped.
func TestDropContained_KeepsWhatIsNotInside(t *testing.T) {
	t.Parallel()
	whole := held(0, "/s", `{"if":{"x":1}}`)
	for name, lookalike := range map[string]ir.UnmodeledEntry{
		"a pointer the value holds nothing at": held(0, "/s/x", `1`),
		"another value":                        held(0, "/s/if/x", `2`),
		"another source":                       held(1, "/s/if/x", `1`),
		"the same pointer":                     held(0, "/s", `{"if":{"x":1}}`),
	} {
		p := ir.Unmodeled{"openapi:s": whole, "openapi:inner": lookalike}
		promotion.DropContained(noPromotions(), &p)
		assert.Equal(t, []string{"openapi:inner", "openapi:s"}, keysOf(p), name)
	}

	inside := ir.Unmodeled{"openapi:s": whole, "openapi:inner": held(0, "/s/if/x", `1`), "openapi:other": held(0, "/o", `1`)}
	promotion.DropContained(noPromotions(), &inside, "openapi:other")
	assert.Equal(t, []string{"openapi:inner", "openapi:other", "openapi:s"}, keysOf(inside),
		"only the entries whole names are asked whether they hold another")

	promotion.DropContained(noPromotions(), &inside, "openapi:s")
	assert.Equal(t, []string{"openapi:other", "openapi:s"}, keysOf(inside))
}

// TestDropContained_KeepsAKeyAPromotionReads holds an entry naming a key the
// policy reads to staying though another holds it whole, unscoped and beneath
// a scope, since a promotion reads the kept entries; a key that only ends in
// such a name, or that a disabled policy reads, is dropped.
func TestDropContained_KeepsAKeyAPromotionReads(t *testing.T) {
	t.Parallel()
	reads := promotion.New(mapping(promotion.TargetRedaction, promotion.ExtensionSource{Key: "x-sensitive"},
		promotion.ExtensionSource{Key: "x-a/b"}))
	branch := held(0, "/s/allOf/1", `{"format":"uuid","x-sensitive":true,"x-a/b":1,"x-notx-sensitive":2}`)
	inner := map[string]ir.UnmodeledEntry{
		"openapi:x-sensitive":             held(0, "/s/allOf/1/x-sensitive", `true`),
		"openapi:schema/x-sensitive":      held(0, "/s/allOf/1/x-sensitive", `true`),
		"openapi:schema/x-a/b":            held(0, "/s/allOf/1/x-a~1b", `1`),
		"openapi:schema/x-notx-sensitive": held(0, "/s/allOf/1/x-notx-sensitive", `2`),
	}
	stays := map[string]bool{
		"openapi:x-sensitive": true, "openapi:schema/x-sensitive": true, "openapi:schema/x-a/b": true,
	}
	for key, entry := range inner {
		p := ir.Unmodeled{"openapi:schema/allOf/1": branch, key: entry}
		promotion.DropContained(reads, &p)
		_, kept := p[key]
		assert.Equal(t, stays[key], kept, key)

		off := ir.Unmodeled{"openapi:schema/allOf/1": branch, key: entry}
		promotion.DropContained(noPromotions(), &off)
		assert.Equal(t, []string{"openapi:schema/allOf/1"}, keysOf(off), "%s, no key read", key)
	}
}

// TestDropContained_AnAssembledEntryHoldsNothing holds a validation-only record,
// which is assembled from several positions and so holds nothing at a pointer
// beneath its own, to being kept whole beside what lies under it.
func TestDropContained_AnAssembledEntryHoldsNothing(t *testing.T) {
	t.Parallel()
	p := ir.Unmodeled{
		"openapi:validationOnly": held(0, "/s", `{"not":{"type":"string"},"if":{"x":1}}`),
		"openapi:not":            held(0, "/s/else", `{"type":"integer"}`),
	}
	promotion.DropContained(noPromotions(), &p)
	assert.Len(t, p, 2)
}

// TestDropContained_LeavesNilAndEmptyAlone holds the degenerate maps to no
// panic and no entry made.
func TestDropContained_LeavesNilAndEmptyAlone(t *testing.T) {
	t.Parallel()
	var nilMap ir.Unmodeled
	promotion.DropContained(noPromotions(), &nilMap)
	assert.Nil(t, nilMap)
	empty := ir.Unmodeled{}
	promotion.DropContained(noPromotions(), &empty)
	assert.Empty(t, empty)
	promotion.DropContained(noPromotions(), nil)
}

// keysOf returns the keys of p in order.
func keysOf(p ir.Unmodeled) []string {
	out := make([]string, 0, len(p))
	for k := range p {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
