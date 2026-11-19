// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

// Find returns the document the table read under name, and false for a name
// it read none under: a lookup only this package's external tests make.
func (t *ReferenceTargetTable) Find(name string) (DocID, bool) {
	for id, d := range t.docs {
		if d.Name == name {
			return DocID(id), true
		}
	}
	return 0, false
}

// MaxPointerTokens is maxPointerTokens, for the external tests that read a
// pointer to its cap.
const MaxPointerTokens = maxPointerTokens

// Steps reports the work the table cost, which only this package's external
// tests read, to try every limit short of it.
func (t *ReferenceTargetTable) Steps() int { return t.steps }

// Passes reports how many passes the table was read in, the last standing,
// which only this package's external tests read.
func (t *ReferenceTargetTable) Passes() int { return t.passes }

// Sat is sat, for the external tests that read the saturating sum.
var Sat = sat

// SatMul is satMul, for the external tests that read the saturating product.
var SatMul = satMul

// Spelling returns c with note told each position a table's walks push a frame
// for, and whether a mapping holds it, so its key is a node at that pointer
// too: what only this package's external tests read, to total the pointers the
// walks spell.
func (c Config) Spelling(note func(at DocumentPointer, member bool)) Config {
	c.pushed = note
	return c
}
