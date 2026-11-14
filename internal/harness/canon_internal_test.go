// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// abOrder is the sequence every comparison row reverses, in either coordinates.
var abOrder = permutation{
	written: sequences{"/components/schemas/AB/allOf": 2},
	moved:   sequences{"/components/schemas/AB/allOf": 2},
}

// declaring is a model of the name holding one member, p, of the target type.
// Its IDs and pointers are the ones the compiler gives a schema declared under
// components.schemas, the member's provenance included, so a shape line that
// kept it would tell two declarations apart.
func declaring(name string, p ir.TypeID) *ir.Model {
	return &ir.Model{
		TypeCommon: ir.TypeCommon{
			ID:   ir.TypeID("t/openapi/components/schemas/" + name),
			Name: ir.Naming{Source: name, Canonical: strings.ToLower(name)},
		},
		Properties: []ir.Property{{
			ID:       ir.PropID("p/openapi/components/schemas/" + name + "/properties/p"),
			Name:     ir.Naming{Source: "p", Canonical: "p"},
			WireName: "p",
			Type:     ir.TypeRef{Target: p},
			Provenance: ir.Provenance{
				Pointer: jsontext.Pointer("/components/schemas/" + name + "/properties/p"),
			},
		}},
	}
}

// composing is the model AB composing A and B in the order given.
func composing(first, second ir.TypeID) *ir.Model {
	return &ir.Model{
		TypeCommon: ir.TypeCommon{
			ID:   "t/openapi/components/schemas/AB",
			Name: ir.Naming{Source: "AB", Canonical: "ab"},
		},
		Mixins: []ir.TypeRef{{Target: first}, {Target: second}},
	}
}

// composedOf is a document holding A and B declaring p as given and AB
// composing them in the order given.
func composedOf(aType, bType ir.TypeID, first, second ir.TypeID) *ir.Document {
	return &ir.Document{Types: ir.TypeRegistry{
		"t/openapi/components/schemas/A":  declaring("A", aType),
		"t/openapi/components/schemas/B":  declaring("B", bType),
		"t/openapi/components/schemas/AB": composing(first, second),
	}}
}

const (
	typeA ir.TypeID = "t/openapi/components/schemas/A"
	typeB ir.TypeID = "t/openapi/components/schemas/B"
)

// TestDiffSequenceInvariants_ComparesTheFlattenedShape pins what a consumer
// reads from the order of a composition. Mixins compare as a multiset, so the
// documents below are equal but for which declaration of p the model takes,
// and only the shape sees that.
func TestDiffSequenceInvariants_ComparesTheFlattenedShape(t *testing.T) {
	t.Parallel()

	t.Run("a member taking the other declaration", func(t *testing.T) {
		t.Parallel()
		asWritten := composedOf("t/prim/string", "t/prim/integer", typeA, typeB)
		reversed := composedOf("t/prim/string", "t/prim/integer", typeB, typeA)

		detail, ok, why := diffSequenceInvariants(asWritten, nil, reversed, nil, abOrder)

		require.Empty(t, why)
		assert.False(t, ok)
		assert.Contains(t, detail, "flattened shape depends on sequence order")
		assert.Contains(t, detail, `member "p"`)
		assert.NotContains(t, detail, "document depends", "the mixins are the same set")
	})

	t.Run("declarations that say the same", func(t *testing.T) {
		t.Parallel()
		// The two declarations differ in their IDs and provenance, which a
		// shape line does not keep.
		asWritten := composedOf("t/prim/string", "t/prim/string", typeA, typeB)
		reversed := composedOf("t/prim/string", "t/prim/string", typeB, typeA)

		detail, ok, why := diffSequenceInvariants(asWritten, nil, reversed, nil, abOrder)

		require.Empty(t, why)
		assert.True(t, ok, detail)
	})
}

// TestDiffSequenceInvariants_ASelfReferenceEndsTheWalk pins that an ID met
// while its own digest is being computed is written erased alone, so a node
// naming itself ends the walk. Without the guard the digest of the model
// never completes, and the walk runs into its depth bound instead of
// returning.
func TestDiffSequenceInvariants_ASelfReferenceEndsTheWalk(t *testing.T) {
	t.Parallel()
	const self ir.TypeID = "t/anon/components/schemas/AB/allOf/0"
	cyclic := func() *ir.Document {
		m := declaring("AB", self)
		m.ID = self
		m.Properties[0].ID = "p/anon/components/schemas/AB/allOf/0/properties/p"
		m.Properties[0].Provenance.Pointer = "/components/schemas/AB/allOf/0/properties/p"
		return &ir.Document{Types: ir.TypeRegistry{self: m}}
	}

	detail, ok, why := diffSequenceInvariants(cyclic(), nil, cyclic(), nil, abOrder)

	require.Empty(t, why)
	assert.True(t, ok, detail)
}

// TestDiffSequenceInvariants_ADocumentItCannotWriteIsReported pins that a
// document the comparison cannot encode is a finding naming the order it came
// from, rather than two empty forms that compare equal, and that one nesting
// past the canonical form's bound is declined: the bound is the harness's, not
// the compiler's.
func TestDiffSequenceInvariants_ADocumentItCannotWriteIsReported(t *testing.T) {
	t.Parallel()
	unencodable := func() *ir.Document {
		doc := composedOf("t/prim/string", "t/prim/string", typeA, typeB)
		doc.Types[typeA].(*ir.Model).Docs.Description = "\xff"
		return doc
	}
	deep := func() *ir.Document {
		doc := composedOf("t/prim/string", "t/prim/string", typeA, typeB)
		value := strings.Repeat("[", maxCanonicalDepth+1) + strings.Repeat("]", maxCanonicalDepth+1)
		doc.Types[typeA].(*ir.Model).Unmodeled = ir.Unmodeled{"openapi:x": {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(value)}}
		return doc
	}
	plain := func() *ir.Document { return composedOf("t/prim/string", "t/prim/string", typeA, typeB) }
	for _, tc := range []struct {
		name          string
		first, second *ir.Document
		prefix        string
		why           Decline
	}{
		{name: "as written, a string that is no UTF-8", first: unencodable(), second: plain(), prefix: "encode as written: "},
		{name: "reversed, a string that is no UTF-8", first: plain(), second: unencodable(), prefix: "encode permuted: "},
		{name: "as written, a value nested past the bound", first: deep(), second: plain(), why: DeclineTooDeep},
		{name: "reversed, a value nested past the bound", first: plain(), second: deep(), why: DeclineTooDeep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok, why := diffSequenceInvariants(tc.first, nil, tc.second, nil, abOrder)

			assert.False(t, ok)
			assert.Equal(t, tc.why, why)
			assert.True(t, strings.HasPrefix(detail, tc.prefix), detail)
		})
	}
}

// TestDiffSequenceInvariants_ForgivesOnlyWhatTheReversalMoves pins each limit
// of what the comparison forgives. Each pair of rows differs in one index,
// which the reversal can have moved in the second row of the pair and cannot
// in the first: an index after another keyword than allOf or required, an
// array kept verbatim off the reversed sequences, and a pointer or ID of
// another source.
func TestDiffSequenceInvariants_ForgivesOnlyWhatTheReversalMoves(t *testing.T) {
	t.Parallel()
	const ab = "/components/schemas/AB/allOf"
	kept := func(name string, at ir.Provenance, value string) func(*ir.Document, int) []ir.Diagnostic {
		return func(doc *ir.Document, _ int) []ir.Diagnostic {
			doc.Types[typeA].(*ir.Model).Unmodeled = ir.Unmodeled{name: {Reason: ir.ReasonVendorExtension,
				Value: ir.RawValue(value), Provenance: at}}
			return nil
		}
	}
	keptAt := func(prefix string) func(*ir.Document, int) []ir.Diagnostic {
		return func(doc *ir.Document, i int) []ir.Diagnostic {
			return kept(prefix+strconv.Itoa(i), ir.Provenance{}, "true")(doc, i)
		}
	}
	keptArray := func(pointer string, source int) func(*ir.Document, int) []ir.Diagnostic {
		return func(doc *ir.Document, i int) []ir.Diagnostic {
			at := ir.Provenance{Source: source, Pointer: jsontext.Pointer(pointer)}
			return kept("x-ext", at, []string{`{"allOf":[1,2]}`, `{"allOf":[2,1]}`}[i])(doc, i)
		}
	}
	// tiedBeside reports at one index in either order, which the reversal moves,
	// so the site is a tie, and changes what a model of source says there.
	tiedBeside := func(source int) func(*ir.Document, int) []ir.Diagnostic {
		return func(doc *ir.Document, i int) []ir.Diagnostic {
			m := declaring("C", "t/prim/string")
			m.Provenance = ir.Provenance{Source: source, Pointer: ab + "/0/properties/p"}
			m.Docs.Description = strconv.Itoa(i)
			doc.Types[m.ID] = m
			return []ir.Diagnostic{{Severity: ir.SeverityWarning, Code: "x/tie",
				Provenance: ir.Provenance{Pointer: ab + "/1/properties/p"}}}
		}
	}
	model := func(prefix string, source int) func(*ir.Document, int) []ir.Diagnostic {
		return func(doc *ir.Document, i int) []ir.Diagnostic {
			m := declaring("C", "t/prim/string")
			m.ID = ir.TypeID(prefix + ab + "/" + strconv.Itoa(i))
			m.Provenance = ir.Provenance{Source: source, Pointer: jsontext.Pointer(ab + "/" + strconv.Itoa(i))}
			doc.Types[m.ID] = m
			return nil
		}
	}
	reported := func(source int) func(*ir.Document, int) []ir.Diagnostic {
		return func(_ *ir.Document, i int) []ir.Diagnostic {
			return []ir.Diagnostic{{Severity: ir.SeverityWarning, Code: "x/at",
				Provenance: ir.Provenance{Source: source, Pointer: jsontext.Pointer(ab + "/" + strconv.Itoa(i))}}}
		}
	}
	for _, tc := range []struct {
		name  string
		edit  func(doc *ir.Document, index int) []ir.Diagnostic
		found bool
	}{
		{"an unmodeled name indexing a oneOf", keptAt("openapi:oneOf/"), true},
		{"an unmodeled name indexing an allOf", keptAt("openapi:allOf/"), false},
		{"a verbatim allOf array off the reversed sequences", keptArray("/components/schemas/A/x-ext", 0), true},
		{"a verbatim allOf array of another source", keptArray("/components/schemas/AB", 1), true},
		{"a verbatim allOf array at a reversed sequence", keptArray("/components/schemas/AB", 0), false},
		{"a node of another source at a tie", tiedBeside(1), true},
		{"a node of the compiled source at a tie", tiedBeside(0), false},
		{"a provenance of another source", model("t/openapi", 1), true},
		{"a provenance of the compiled source", model("t/openapi", 0), false},
		{"an ID of another file", model("t/openapi@b.yaml", 0), true},
		{"a diagnostic of another source", reported(1), true},
		{"a diagnostic of the compiled source", reported(0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := composedOf("t/prim/string", "t/prim/string", typeA, typeB)
			second := composedOf("t/prim/string", "t/prim/string", typeA, typeB)
			firstDiags, secondDiags := tc.edit(first, 0), tc.edit(second, 1)

			detail, ok, why := diffSequenceInvariants(first, firstDiags, second, secondDiags, abOrder)

			require.Empty(t, why)
			assert.Equal(t, !tc.found, ok, detail)
		})
	}
}
