package annotation

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// sourced is the Locator for a compile with no overlay: every position belongs
// to the one source at index.
func sourced(index int) Locator {
	return func(pointer jsontext.Pointer, _ ...jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Source: index, Pointer: string(pointer)}
	}
}

// overlaid is the Locator for a compile where an overlay touched exactly
// pointers: each resolves to source 1, as ProvenanceAt resolves a position the
// overlay introduced or rewrote, and every other pointer resolves to source 0,
// the base document.
func overlaid(pointers ...jsontext.Pointer) Locator {
	return func(pointer jsontext.Pointer, _ ...jsontext.Pointer) ir.Provenance {
		if slices.Contains(pointers, pointer) {
			return ir.Provenance{Source: 1, Pointer: string(pointer)}
		}
		return ir.Provenance{Source: 0, Pointer: string(pointer)}
	}
}

// recordedCall is one (pointer, from) pair a recording Locator was asked
// about.
type recordedCall struct {
	pointer jsontext.Pointer
	from    []jsontext.Pointer
}

// recording is a Locator that appends every call it receives to calls, in call
// order, and answers source 1 when from is non-empty and source 0 otherwise —
// enough to tell a combined entry's call, made with the keywords it holds,
// apart from a single position's, made with none. It exists to let a test
// assert what a reader asked a Locator, rather than only what answer came
// back.
func recording(calls *[]recordedCall) Locator {
	return func(pointer jsontext.Pointer, from ...jsontext.Pointer) ir.Provenance {
		*calls = append(*calls, recordedCall{pointer: pointer, from: slices.Clone(from)})
		source := 0
		if len(from) > 0 {
			source = 1
		}
		return ir.Provenance{Source: source, Pointer: string(pointer)}
	}
}
