// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import "github.com/speakeasy-api/openapi/jsonschema/oas3"

// Reads returns how many schema objects t has read, for the tests holding the
// table to one read of each.
func Reads(t *Table) int { return t.reads }

// Lookups returns how many statements t has looked up, for the tests holding
// a schema whose statement is built to reading it with none.
func Lookups(t *Table) int { return t.lookups }

// Compared returns how many keys s compares name with to find it, for the
// tests holding a wide object's lookups to its index.
func Compared(s *Schema, name string) int {
	_, n := s.find(name)
	return n
}

// ModelKeywords returns the keywords fill reads from the parser's model, in
// modelKeywords' order.
func ModelKeywords() []string {
	out := make([]string, len(modelKeywords))
	for i, k := range modelKeywords {
		out[i] = k.name
	}
	return out
}

// FillTruncated returns what fill reads of m into a Truncated schema whose
// keys list nothing.
func FillTruncated(m *oas3.Schema) *Schema {
	s := &Schema{Truncated: true}
	fill(s, m)
	return s
}
