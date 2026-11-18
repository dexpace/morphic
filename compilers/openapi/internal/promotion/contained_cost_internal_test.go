// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestDropContained_AsksEachEntryOfItsAncestorsOnly pins that the entries an
// entry is asked against are the ones lying above it, not every entry that can
// hold one: from 2n entries to 4n the questions asked grow at most 2.5 times
// what they grow from n to 2n, whether the holders are listed or every entry is
// asked.
func TestDropContained_AsksEachEntryOfItsAncestorsOnly(t *testing.T) {
	t.Parallel()
	for name, listed := range map[string]bool{"holders listed": true, "every entry a holder": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var asked [3]int
			for k, n := range []int{100, 200, 400} {
				u := ir.Unmodeled{"openapi:s": {Value: ir.RawValue(`{}`), Provenance: ir.Provenance{Pointer: "/s"}}}
				whole := []string{"openapi:s"}
				for i := range n {
					key := fmt.Sprintf("openapi:allOf/%d", i)
					u[key] = ir.UnmodeledEntry{
						Value:      ir.RawValue(`{"x":1}`),
						Provenance: ir.Provenance{Pointer: jsontext.Pointer(fmt.Sprintf("/s/allOf/%d", i))},
					}
					whole = append(whole, key)
				}
				if !listed {
					whole = nil
				}
				dropContained(New(ExtensionPromotions{Disabled: true}), &u, func(outer, inner ir.UnmodeledEntry) bool {
					asked[k]++
					return holdsWhole(outer, inner)
				}, whole)
				assert.Len(t, u, n+1, "nothing lies beneath another entry")
			}
			assert.Positive(t, asked[0], "no entry was asked against another: the shape reaches no question")
			assert.LessOrEqual(t, float64(asked[2]-asked[1]), 2.5*float64(asked[1]-asked[0]),
				"questions at 100, 200 and 400 entries: %v", asked)
		})
	}
}

// TestDropContained_DropsWhatAnyEntryHolds pins the drop to the plain reading
// of its definition, each entry asked of every other as the set stood, over
// entries that nest in a chain, lie in other sources, differ in value, and
// sit at one pointer: what a dropped entry held is dropped with it, so the
// order the entries are visited in changes nothing.
func TestDropContained_DropsWhatAnyEntryHolds(t *testing.T) {
	t.Parallel()
	at := func(source int, pointer, value string) ir.UnmodeledEntry {
		return ir.UnmodeledEntry{Value: ir.RawValue(value), Provenance: ir.Provenance{Source: source, Pointer: jsontext.Pointer(pointer)}}
	}
	set := func() ir.Unmodeled {
		return ir.Unmodeled{
			"a":      at(0, "/s", `{"b":{"c":{"d":1}}}`),
			"b":      at(0, "/s/b", `{"c":{"d":1}}`),
			"c":      at(0, "/s/b/c", `{"d":1}`),
			"d":      at(0, "/s/b/c/d", `1`),
			"other":  at(1, "/s/b/c/d", `1`),
			"differ": at(0, "/s/b/c/e", `2`),
			"twin":   at(0, "/s", `{"b":{"c":{"d":1}}}`),
			"apart":  at(0, "/t", `{"x":1}`),
			"inside": at(0, "/t/x", `1`),
			"root":   at(0, "", `{"s":{"b":{"c":{"d":1}}}}`),
		}
	}
	plain := set()
	for key, inner := range set() {
		for other, outer := range set() {
			if other != key && holdsWhole(outer, inner) {
				delete(plain, key)
				break
			}
		}
	}
	for name, whole := range map[string][]string{"every entry a holder": nil, "listed holders": {"root", "a", "b", "c", "apart"}} {
		got := set()
		DropContained(New(ExtensionPromotions{Disabled: true}), &got, whole...)
		assert.Equal(t, plain, got, name)
	}
	assert.NotEmpty(t, plain)
	assert.Less(t, len(plain), len(set()), "the set holds something")
}
