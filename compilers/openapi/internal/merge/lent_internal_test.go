// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// lendable is each detail a referent may lend, with how a declaration gives it
// and the name a report says it by.
var lendable = []struct {
	detail Details
	name   string
	give   func(*ir.Property, string)
}{
	{DetailTitle, "title", func(p *ir.Property, s string) { p.Docs.Summary = s }},
	{DetailDescription, "description", func(p *ir.Property, s string) { p.Docs.Description = s }},
	{DetailExternalDocs, "external docs", func(p *ir.Property, s string) { p.Docs.ExternalDocs = []ir.Link{{URL: s}} }},
	{DetailDeprecation, "deprecation", func(p *ir.Property, s string) { p.Deprecation = &ir.Deprecation{Message: s} }},
	{DetailDefault, "default", func(p *ir.Property, s string) { p.Default = &ir.Value{Kind: ir.ValueString, String: s} }},
}

// givingAt returns a declaration at pointer, of a type taking any value, giving
// detail as value, and rendered as `{}`.
func givingAt(pointer jsontext.Pointer, give func(*ir.Property, string), value string) declaration {
	declarationValue := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveAny), Provenance: ptrAt(pointer)},
		source: written(`{}`)}
	give(&declarationValue.property, value)
	return declarationValue
}

// TestFold_ABorrowedDetailIsNoConflict pins GitHub #2152 at the fold: an
// earlier declaration whose detail its referent lent wrote nothing the later
// one sets aside, so it is neither reported nor kept, and the field keeps the
// later value. One writing that detail, or one the walk cannot read, is
// reported and kept, whatever else it borrowed.
func TestFold_ABorrowedDetailIsNoConflict(t *testing.T) {
	t.Parallel()
	for _, tc := range lendable {
		for _, walk := range []struct {
			name     string
			borrowed Details
			unplaced bool
			reported bool
		}{
			{"borrowed", tc.detail, false, false},
			{"every other borrowed", everyDetail &^ tc.detail, false, true},
			{"unread", tc.detail, true, true},
		} {
			stubbedValue, recorded := stubMerger(narrowingRegistry())
			stubbedValue.walk.borrowed = map[jsontext.Pointer]Details{"/first": walk.borrowed}
			if walk.unplaced {
				stubbedValue.walk.unplaced = []jsontext.Pointer{"/first"}
			}
			later := givingAt("/later", tc.give, "later")

			got := stubbedValue.fold([]declaration{givingAt("/first", tc.give, "first"), later})

			want := later.property
			assert.Equal(t, want.Docs, got.Docs, "%s, %s", tc.name, walk.name)
			assert.Equal(t, want.Deprecation, got.Deprecation, "%s, %s", tc.name, walk.name)
			assert.Equal(t, want.Default, got.Default, "%s, %s", tc.name, walk.name)
			if !walk.reported {
				assert.Empty(t, *recorded, "%s, %s", tc.name, walk.name)
				assert.Empty(t, got.Unmodeled, "%s, %s", tc.name, walk.name)
				continue
			}
			require.Len(t, *recorded, 1, "%s, %s", tc.name, walk.name)
			assert.Contains(t, (*recorded)[0].Message, "give its "+tc.name+" differently", walk.name)
			assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/first", "%s, %s", tc.name, walk.name)
		}
	}
}

// TestFold_AWrittenDetailIsKeptOverALentOne pins GitHub #2152's rule: in
// either order, the field keeps the detail a declaration wrote over the one
// another's referent lent, which set aside nothing written, so nothing is
// reported or kept beside the field, and it says it kept the detail written.
func TestFold_AWrittenDetailIsKeptOverALentOne(t *testing.T) {
	t.Parallel()
	for _, tc := range lendable {
		lent, writing := givingAt("/lent", tc.give, "lent"), givingAt("/written", tc.give, "written")
		for _, order := range [][]declaration{{lent, writing}, {writing, lent}} {
			stubbedValue, _ := stubMerger(narrowingRegistry())
			stubbedValue.walk.borrowed = map[jsontext.Pointer]Details{"/lent": tc.detail}

			got, written, diags := stubbedValue.Merger.fold(order)

			first := order[0].property.Provenance.Pointer
			assert.Equal(t, writing.property.Docs, got.Docs, "%s, %s first", tc.name, first)
			assert.Equal(t, writing.property.Deprecation, got.Deprecation, "%s, %s first", tc.name, first)
			assert.Equal(t, writing.property.Default, got.Default, "%s, %s first", tc.name, first)
			assert.Equal(t, tc.detail, written, "%s, %s first", tc.name, first)
			assert.Empty(t, diags, "%s, %s first", tc.name, first)
			assert.Empty(t, got.Unmodeled, "%s, %s first", tc.name, first)
		}
	}
}

// TestFold_ALentDetailOnANodeTheFieldLeavesOutIsKept pins that a lent detail
// losing to a written one is reported, and its declaration kept, where the
// referent lending it is an anonymous node the field's type leaves out, which
// pruning may remove with the detail on it. Nothing is reported where the
// referent is a declared type, where the field takes it, where the two details
// are alike, or where an entry brings the declaration, whose model references it.
func TestFold_ALentDetailOnANodeTheFieldLeavesOutIsKept(t *testing.T) {
	t.Parallel()
	const lender = "type/anonymous/lender"
	for _, tc := range lendable {
		for _, row := range []struct {
			name      string
			anonymous bool
			brought   bool
			value     string
			written   ir.TypeRef
			reported  bool
		}{
			{"left out", true, false, "lent", primitiveRef(ir.PrimitiveString), true},
			{"declared", false, false, "lent", primitiveRef(ir.PrimitiveString), false},
			{"taken", true, false, "lent", ir.TypeRef{Target: lender}, false},
			{"alike", true, false, "written", primitiveRef(ir.PrimitiveString), false},
			{"brought", true, true, "lent", primitiveRef(ir.PrimitiveString), false},
		} {
			lent, writing := givingAt("/lent", tc.give, row.value), givingAt("/written", tc.give, "written")
			lent.property.Type, writing.property.Type = ir.TypeRef{Target: lender}, row.written
			if row.brought {
				lent.via = &composedEntry{target: "type/part", at: "/allOf/0"}
			}
			for _, order := range [][]declaration{{lent, writing}, {writing, lent}} {
				reg := narrowingRegistry()
				reg[lender] = &ir.Scalar{ID: lender, Anonymous: row.anonymous, Base: new(primitiveRef(ir.PrimitiveAny))}
				stubbedValue, _ := stubMerger(reg)
				stubbedValue.walk.borrowed = map[jsontext.Pointer]Details{"/lent": tc.detail}

				got, _, diags := stubbedValue.foldAt(order, "/p", nil)

				at := fmt.Sprintf("%s, %s, %s first", tc.name, row.name, order[0].property.Provenance.Pointer)
				assert.Equal(t, writing.property.Docs, got.Docs, at)
				assert.Equal(t, writing.property.Deprecation, got.Deprecation, at)
				if !row.reported {
					assert.Empty(t, diags, at)
					assert.Empty(t, got.Unmodeled, at)
					continue
				}
				require.Len(t, diags, 1, at)
				assert.Equal(t, jsontext.Pointer("/lent"), diags[0].Provenance.Pointer, at)
				assert.Contains(t, diags[0].Message, "give its "+tc.name+" differently; kept the one the declaration at /written "+
					"gives over the one the declaration at /lent borrows from a schema the field does not take as its type; "+
					"that declaration is kept verbatim under Unmodeled", at)
				assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/lent", at)
			}
		}
	}
}

// TestFold_LentDetailsTieOnlyWhereNoDeclarationWritesOne pins what GitHub
// #2152 leaves to declaration order: of declarations each borrowing a detail,
// none writing one, the last is kept and an earlier one lending another is
// reported as borrowed, not given, and kept, as two written are. A declaration
// writing the detail outvotes every one of them, so none is reported.
func TestFold_LentDetailsTieOnlyWhereNoDeclarationWritesOne(t *testing.T) {
	t.Parallel()
	for _, tc := range lendable {
		first, later := givingAt("/first", tc.give, "first"), givingAt("/later", tc.give, "later")
		borrowed := map[jsontext.Pointer]Details{"/first": tc.detail, "/later": tc.detail}
		stubbedValue, _ := stubMerger(narrowingRegistry())
		stubbedValue.walk.borrowed = borrowed

		got, written, diags := stubbedValue.Merger.fold([]declaration{first, later})

		assert.Equal(t, later.property.Docs, got.Docs, tc.name)
		assert.Equal(t, later.property.Deprecation, got.Deprecation, tc.name)
		assert.Equal(t, later.property.Default, got.Default, tc.name)
		assert.Zero(t, written, "%s: the field keeps it from no declaration writing it", tc.name)
		require.Len(t, diags, 1, tc.name)
		assert.Contains(t, diags[0].Message, "borrow its "+tc.name+" differently from the schemas their $refs name; "+
			"kept the one the declaration at /later borrows over the one at /first", tc.name)
		assert.NotContains(t, diags[0].Message, "give its", tc.name)
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/first", tc.name)

		writing := givingAt("/written", tc.give, "written")
		stubbedValue, _ = stubMerger(narrowingRegistry())
		stubbedValue.walk.borrowed = borrowed

		got, written, diags = stubbedValue.Merger.fold([]declaration{writing, first, later})

		assert.Equal(t, writing.property.Docs, got.Docs, tc.name)
		assert.Equal(t, writing.property.Deprecation, got.Deprecation, tc.name)
		assert.Equal(t, writing.property.Default, got.Default, tc.name)
		assert.Equal(t, tc.detail, written, tc.name)
		assert.Empty(t, diags, "%s: no lent detail is a party to a report beside a written one", tc.name)
		assert.Empty(t, got.Unmodeled, tc.name)
	}
}

// TestFold_ADetailNoReferentLendsIsAlwaysWritten pins that a borrowed set
// says nothing of XML hints, which no referent lends: two declarations giving
// them differently are reported though the earlier borrowed every detail.
func TestFold_ADetailNoReferentLendsIsAlwaysWritten(t *testing.T) {
	t.Parallel()
	stubbedValue, recorded := stubMerger(narrowingRegistry())
	stubbedValue.walk.borrowed = map[jsontext.Pointer]Details{"/first": everyDetail}
	xmlNamed := func(p *ir.Property, s string) { p.XML = &ir.XMLHints{Name: s} }

	got := stubbedValue.fold([]declaration{givingAt("/first", xmlNamed, "first"), givingAt("/later", xmlNamed, "later")})

	assert.Equal(t, "later", got.XML.Name)
	require.Len(t, *recorded, 1)
	assert.Contains(t, (*recorded)[0].Message, "give its xml differently")
}

// TestFold_ReturnsTheDetailsItKeepsFromADeclarationWritingThem pins what a
// settled member says of itself to the fold reading it next (Declared): each
// detail the field keeps from a declaration that wrote it, and none it keeps
// from one that borrowed it or that no declaration gives. A settled declaration
// says it through what it carries, which the walk is not asked for, and every
// other declaration is read once.
func TestFold_ReturnsTheDetailsItKeepsFromADeclarationWritingThem(t *testing.T) {
	t.Parallel()
	stubbedValue, _ := stubMerger(narrowingRegistry())
	stubbedValue.walk.borrowed = map[jsontext.Pointer]Details{"/second": DetailTitle}
	first := givingAt("/first", lendable[1].give, "described")
	lendable[3].give(&first.property, "deprecated")
	second := givingAt("/second", lendable[0].give, "titled")
	lendable[2].give(&second.property, "https://linked.example")
	settled := givingAt("/settled", lendable[4].give, "defaulted")
	settled.settled = true

	_, written, _ := stubbedValue.Merger.fold([]declaration{first, second, settled})

	assert.Equal(t, DetailDescription|DetailDeprecation|DetailExternalDocs, written)
	assert.Equal(t, 2, stubbedValue.walk.reads, "first and second, once each")

	settled.written = DetailDefault
	_, written, _ = stubbedValue.Merger.fold([]declaration{first, second, settled})
	assert.Equal(t, DetailDescription|DetailDeprecation|DetailExternalDocs|DetailDefault, written)
}
