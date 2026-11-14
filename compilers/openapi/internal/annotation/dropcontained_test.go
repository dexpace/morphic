// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

// held is an entry of source src at pointer, holding the JSON value.
func held(src int, pointer, value string) ir.UnmodeledEntry {
	return ir.UnmodeledEntry{
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue(value),
		Provenance: ir.Provenance{Source: src, Pointer: jsonPointer(pointer)},
	}
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
	annotation.DropContained(&p)
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
		annotation.DropContained(&p)
		assert.Equal(t, []string{"openapi:inner", "openapi:s"}, keysOf(p), name)
	}

	inside := ir.Unmodeled{"openapi:s": whole, "openapi:inner": held(0, "/s/if/x", `1`), "openapi:other": held(0, "/o", `1`)}
	annotation.DropContained(&inside, "openapi:other")
	assert.Equal(t, []string{"openapi:inner", "openapi:other", "openapi:s"}, keysOf(inside),
		"only the entries whole names are asked whether they hold another")

	annotation.DropContained(&inside, "openapi:s")
	assert.Equal(t, []string{"openapi:other", "openapi:s"}, keysOf(inside))
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
	annotation.DropContained(&p)
	assert.Len(t, p, 2)
}

// TestDropContained_LeavesNilAndEmptyAlone holds the degenerate maps to no
// panic and no entry made.
func TestDropContained_LeavesNilAndEmptyAlone(t *testing.T) {
	t.Parallel()
	var nilMap ir.Unmodeled
	annotation.DropContained(&nilMap)
	assert.Nil(t, nilMap)
	empty := ir.Unmodeled{}
	annotation.DropContained(&empty)
	assert.Empty(t, empty)
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

// jsonPointer is s as a pointer.
func jsonPointer(s string) jsontext.Pointer { return jsontext.Pointer(s) }
