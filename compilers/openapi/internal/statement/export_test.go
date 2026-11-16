// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

// Reads returns how many schema objects t has read, for the tests holding the
// table to one read of each.
func Reads(t *Table) int { return t.reads }

// Compared returns how many keys s compares name with to find it, for the
// tests holding a wide object's lookups to its index.
func Compared(s *Schema, name string) int {
	_, n := s.find(name)
	return n
}
