// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// readerCase is one reader of a slot: what it returns whole, and one plain
// value of that, which a schema writing the slot's keywords sets.
type readerCase struct {
	name  string
	whole func(*statement.Schema) any
	field func(*statement.Schema) any
	want  any
}

var (
	enumNode  = &yaml.Node{Kind: yaml.ScalarNode, Value: "a"}
	constNode = &yaml.Node{Kind: yaml.ScalarNode, Value: "b"}
	defNode   = &yaml.Node{Kind: yaml.ScalarNode, Value: "c"}
	minItems  = int64(2)
)

// readerCases lists every reader of a slot, each beside what it reads of
// writtenSlots.
func readerCases() []readerCase {
	return []readerCase{
		{"ArrayKeywords", func(s *statement.Schema) any { return s.ArrayKeywords() },
			func(s *statement.Schema) any { return s.ArrayKeywords().MinItems }, &minItems},
		{"ObjectKeywords", func(s *statement.Schema) any { return s.ObjectKeywords() },
			func(s *statement.Schema) any { return s.ObjectKeywords().Required }, []string{"id"}},
		{"TextKeywords", func(s *statement.Schema) any { return s.TextKeywords() },
			func(s *statement.Schema) any { return s.TextKeywords().Pattern }, "^a"},
		{"ChoiceKeywords", func(s *statement.Schema) any { return s.ChoiceKeywords() },
			func(s *statement.Schema) any { return s.ChoiceKeywords().OneOf[0].Name }, "one"},
		{"LogicKeywords", func(s *statement.Schema) any { return s.LogicKeywords() },
			func(s *statement.Schema) any { return s.LogicKeywords().Dependencies }, enumNode},
		{"Types", func(s *statement.Schema) any { return s.Types() },
			func(s *statement.Schema) any { return s.Types() }, []statement.JSONSchemaTypeName{statement.KindString}},
		{"Enum", func(s *statement.Schema) any { return s.Enum() },
			func(s *statement.Schema) any { return s.Enum() }, []*yaml.Node{enumNode}},
		{"Const", func(s *statement.Schema) any { return s.Const() },
			func(s *statement.Schema) any { return s.Const() }, constNode},
		{"FormatName", func(s *statement.Schema) any { return s.FormatName() },
			func(s *statement.Schema) any { return s.FormatName() }, "uuid"},
		{"PrefixItems", func(s *statement.Schema) any { return s.PrefixItems() },
			func(s *statement.Schema) any { return s.PrefixItems()[0].Name }, "first"},
		{"Discriminator", func(s *statement.Schema) any { return s.Discriminator() },
			func(s *statement.Schema) any { return s.Discriminator().PropertyName }, "kind"},
		{"Default", func(s *statement.Schema) any { return s.Default() },
			func(s *statement.Schema) any { return s.Default() }, defNode},
		{"XML", func(s *statement.Schema) any { return s.XML() },
			func(s *statement.Schema) any { return *s.XML().Name }, "tag"},
		{"ExternalDocs", func(s *statement.Schema) any { return s.ExternalDocs() },
			func(s *statement.Schema) any { return s.ExternalDocs().URL }, "https://example.com"},
	}
}

// writtenSlots returns a schema writing a keyword of every slot a reader
// reads.
func writtenSlots() *statement.Schema {
	name := "tag"
	return &statement.Schema{
		Kinds:  &statement.Kinds{Types: []statement.JSONSchemaTypeName{statement.KindString}},
		Values: &statement.Values{Enum: []*yaml.Node{enumNode}, Const: constNode},
		Format: &statement.Format{Name: "uuid"},
		Text:   &statement.Text{Pattern: "^a"},
		Array:  &statement.Array{MinItems: &minItems, PrefixItems: []statement.Subschema{{Name: "first"}}},
		Object: &statement.Object{Required: []string{"id"}, Discriminator: &statement.Discriminator{PropertyName: "kind"}},
		Choice: &statement.Choice{OneOf: []statement.Subschema{{Name: "one"}}},
		Logic:  &statement.Logic{Dependencies: enumNode},
		Notes: &statement.Notes{Default: defNode, XML: &statement.XML{Name: &name},
			ExternalDocs: &statement.ExternalDocs{URL: "https://example.com"}},
	}
}

// TestSchema_ReadersOfANilSchemaAndAnUnwrittenSlotReadNothing pins that a
// reader answers the zero value for a nil schema, and for one whose slot is
// nil, as each caller read the slot before the readers were methods.
func TestSchema_ReadersOfANilSchemaAndAnUnwrittenSlotReadNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range readerCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var none *statement.Schema
			assert.Zero(t, tc.whole(none), "a nil schema writes nothing")
			assert.Zero(t, tc.whole(&statement.Schema{}), "a schema with its slot nil writes nothing")
		})
	}
}

// TestSchema_ReadersReadWhatTheirSlotHolds pins that a reader returns the
// value its slot holds, not the zero a nil one reads as.
func TestSchema_ReadersReadWhatTheirSlotHolds(t *testing.T) {
	t.Parallel()
	for _, tc := range readerCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.field(writtenSlots()))
		})
	}
}
