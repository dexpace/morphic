// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestHoldsWhole_ReadsNothingBeneathANonObjectOrAnInvalidValue holds the
// containment test to false where the outer value is no object at a token
// the inner pointer passes, and where a value is no JSON value.
func TestHoldsWhole_ReadsNothingBeneathANonObjectOrAnInvalidValue(t *testing.T) {
	t.Parallel()
	at := func(pointer, value string) ir.UnmodeledEntry {
		return ir.UnmodeledEntry{Value: ir.RawValue(value), Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer)}}
	}
	assert.False(t, holdsWhole(at("/s", `[1]`), at("/s/0", `1`)), "a list is no object")
	assert.False(t, holdsWhole(at("/s", `{"x":1}`), at("/s/x", `{`)), "no JSON value")
	assert.True(t, holdsWhole(at("/s", `{"x":{"b":1,"a":2}}`), at("/s/x", `{"a":2,"b":1}`)), "compared canonically")
}
