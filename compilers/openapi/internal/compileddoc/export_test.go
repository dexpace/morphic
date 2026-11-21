// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compileddoc

import "github.com/dexpace/morphic/ir"

// CountFlattenings makes compiledDocumentView count each whole flattening it
// takes from now on, and returns the count, so an external test can hold a read
// that must name one member to flattening no model.
func CountFlattenings(compiledDocumentView *CompiledDocumentView) *int {
	n := new(int)
	flatten := compiledDocumentView.flatten
	compiledDocumentView.flatten = func(types ir.TypeRegistry, id ir.TypeID) ir.Flattening {
		*n++
		return flatten(types, id)
	}
	return n
}
