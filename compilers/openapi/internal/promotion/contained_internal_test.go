// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestHoldsWhole_ReadsAnObjectsMembersAndAListsElements holds the containment
// test to what the outer value holds at each token the inner pointer passes:
// an object's member, or a list's element at an index RFC 6901 spells, and
// nothing beneath a scalar, past a list's end, at an index spelt otherwise, or
// where a value is no JSON value.
func TestHoldsWhole_ReadsAnObjectsMembersAndAListsElements(t *testing.T) {
	t.Parallel()
	at := func(pointer, value string) ir.UnmodeledEntry {
		return ir.UnmodeledEntry{Value: ir.RawValue(value), Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer)}}
	}
	assert.True(t, holdsWhole(at("/s", `{"allOf":[{"x":1},{"y":2}]}`), at("/s/allOf/1/y", `2`)), "through a list")
	assert.True(t, holdsWhole(at("/s", `[[0,{"x":1}]]`), at("/s/0/1", `{"x":1}`)), "a list in a list")
	for name, pointer := range map[string]string{
		"past the end": "/s/2", "a leading zero": "/s/01", "a sign": "/s/+1", "a negative index": "/s/-1",
		"no index": "/s/a",
	} {
		assert.False(t, holdsWhole(at("/s", `[1,1]`), at(pointer, `1`)), name)
	}
	assert.False(t, holdsWhole(at("/s", `{"x":1}`), at("/s/x/y", `1`)), "nothing beneath a scalar")
	assert.False(t, holdsWhole(at("/s", `[1,`), at("/s/0", `1`)), "a list that is no JSON value")
	assert.False(t, holdsWhole(at("/s", `{"x":1`), at("/s/x", `1`)), "an object that is no JSON value")
	assert.False(t, holdsWhole(at("/s", `{"x":1}`), at("/s/x", `{`)), "no JSON value")
	assert.True(t, holdsWhole(at("/s", `{"x":{"b":1,"a":2}}`), at("/s/x", `{"a":2,"b":1}`)), "compared canonically")
}
