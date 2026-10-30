// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

// Find returns the document the table read under name, and false for a name
// it read none under: a lookup only this package's external tests make.
func (t *Table) Find(name string) (DocID, bool) {
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
