// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

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

		detail, ok := diffSequenceInvariants(asWritten, nil, reversed, nil, abOrder)

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

		detail, ok := diffSequenceInvariants(asWritten, nil, reversed, nil, abOrder)

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

	detail, ok := diffSequenceInvariants(cyclic(), nil, cyclic(), nil, abOrder)

	assert.True(t, ok, detail)
}

// TestDiffSequenceInvariants_ADocumentItCannotWriteIsReported pins that a
// document the comparison cannot write is a finding naming the order it came
// from, rather than two empty forms that compare equal.
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
	}{
		{"as written, a string that is no UTF-8", unencodable(), plain(), "encode as written: "},
		{"reversed, a string that is no UTF-8", plain(), unencodable(), "encode permuted: "},
		{"as written, a value nested past the bound", deep(), plain(), "encode as written: " + errTooDeep.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := diffSequenceInvariants(tc.first, nil, tc.second, nil, abOrder)

			assert.False(t, ok)
			assert.True(t, strings.HasPrefix(detail, tc.prefix), detail)
		})
	}
}
