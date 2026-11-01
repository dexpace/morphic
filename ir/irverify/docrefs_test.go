// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_DocRefs drives each way a span can be wrong, and the shapes that
// are not: leaf names a type that exists, so only the span is ever in question.
func TestVerify_DocRefs(t *testing.T) {
	t.Parallel()
	const pet = "see Pet" // 7 bytes
	const accented = "é and Pet"
	span := func(start, end int) ir.DocRef { return ir.DocRef{Start: start, End: end, Type: "t/x/Leaf"} }
	for _, tc := range []struct {
		name string
		docs ir.Docs
		want map[string]int // violation code to how many; nil means clean
	}{
		{name: "no references", docs: ir.Docs{Description: pet}},
		{name: "a span inside the text", docs: ir.Docs{Description: pet, Refs: []ir.DocRef{span(4, 7)}}},
		{name: "a span opening the text", docs: ir.Docs{Description: pet, Refs: []ir.DocRef{span(0, 3)}}},
		{name: "the whole text", docs: ir.Docs{Description: pet, Refs: []ir.DocRef{span(0, 7)}}},
		{name: "adjacent spans", docs: ir.Docs{Description: pet, Refs: []ir.DocRef{span(0, 4), span(4, 7)}}},
		{name: "a span on a multi-byte rune", docs: ir.Docs{Description: accented, Refs: []ir.DocRef{span(0, 2), span(7, 10)}}},

		{"a span before the text", ir.Docs{Description: pet, Refs: []ir.DocRef{span(-1, 3)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"an empty span", ir.Docs{Description: pet, Refs: []ir.DocRef{span(3, 3)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"an inverted span", ir.Docs{Description: pet, Refs: []ir.DocRef{span(5, 3)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"a span past the text", ir.Docs{Description: pet, Refs: []ir.DocRef{span(4, 8)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"a span on no text", ir.Docs{Refs: []ir.DocRef{span(0, 1)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"a span starting inside a rune", ir.Docs{Description: accented, Refs: []ir.DocRef{span(1, 5)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"a span ending inside a rune", ir.Docs{Description: accented, Refs: []ir.DocRef{span(0, 1)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"overlapping spans", ir.Docs{Description: pet, Refs: []ir.DocRef{span(0, 5), span(4, 7)}},
			map[string]int{"ir/doc-ref-order": 1}},
		{"spans out of order", ir.Docs{Description: pet, Refs: []ir.DocRef{span(4, 7), span(0, 3)}},
			map[string]int{"ir/doc-ref-order": 1}},
		{"a span inside the one before it is measured from the longer", ir.Docs{Description: pet,
			Refs: []ir.DocRef{span(0, 7), span(1, 2), span(3, 4)}}, map[string]int{"ir/doc-ref-order": 2}},
		{"one bad span is one finding", ir.Docs{Description: pet, Refs: []ir.DocRef{span(0, 3), span(5, 99), span(4, 7)}},
			map[string]int{"ir/doc-ref-span": 1}},
		{"a span naming no type", ir.Docs{Description: pet, Refs: []ir.DocRef{{Start: 0, End: 3}}},
			map[string]int{"ir/empty-type-ref": 1}},
		{"a span naming a type nothing declares", ir.Docs{Description: pet, Refs: []ir.DocRef{{Start: 0, End: 3, Type: "t/x/Gone"}}},
			map[string]int{"ir/dangling-type-ref": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &ir.Model{ID: "t/x/M", Name: ir.Naming{Source: "M", Canonical: "m"}, Docs: tc.docs}
			got := map[string]int{}
			for _, v := range irverify.Verify(closedDoc(m)) {
				if strings.Contains(v.Path, ".Refs") {
					got[v.Code]++
				}
			}
			if tc.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestVerify_DocRefsAreHeldWhereverDocsSit pins that the check keys on the type
// and not on a field: a Docs is held on a type, on a member inside one, and on
// a server, whose field is not even named Docs.
func TestVerify_DocRefsAreHeldWhereverDocsSit(t *testing.T) {
	t.Parallel()
	bad := ir.Docs{Description: "x", Refs: []ir.DocRef{{Start: 0, End: 9, Type: "t/x/Leaf"}}}
	doc := closedDoc(&ir.Enum{
		ID:      "t/x/E",
		Name:    ir.Naming{Source: "E", Canonical: "e"},
		Docs:    bad,
		Members: []ir.EnumMember{{ID: "t/x/E#m", Name: ir.Naming{Source: "m", Canonical: "m"}, Docs: bad}},
	})
	doc.Servers = map[ir.ServerID]ir.Server{"server/a": {ID: "server/a", URLTemplate: "https://a.example", Description: bad}}
	assert.ElementsMatch(t, []string{
		`doc.Types[t/x/E].Docs.Refs[0]`,
		`doc.Types[t/x/E].Members[0].Docs.Refs[0]`,
		`doc.Servers[server/a].Description.Refs[0]`,
	}, violationPaths(irverify.Verify(doc), "ir/doc-ref-span"))
}
