package annotation

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// sourced is the Locator for a compile with no overlay: every position belongs
// to the one source at index.
func sourced(index int) Locator {
	return func(pointer jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Source: index, Pointer: string(pointer)}
	}
}

// overlaid is the Locator for a compile where an overlay touched exactly
// pointers: each resolves to source 1, as ProvenanceAt resolves a position the
// overlay introduced or rewrote, and every other pointer resolves to source 0,
// the base document.
func overlaid(pointers ...jsontext.Pointer) Locator {
	return func(pointer jsontext.Pointer) ir.Provenance {
		if slices.Contains(pointers, pointer) {
			return ir.Provenance{Source: 1, Pointer: string(pointer)}
		}
		return ir.Provenance{Source: 0, Pointer: string(pointer)}
	}
}
