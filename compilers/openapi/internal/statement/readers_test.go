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
		{"ArrayKeywords", func(schema *statement.Schema) any { return schema.ArrayKeywords() },
			func(schema *statement.Schema) any { return schema.ArrayKeywords().MinItems }, &minItems},
		{"ObjectKeywords", func(schema *statement.Schema) any { return schema.ObjectKeywords() },
			func(schema *statement.Schema) any { return schema.ObjectKeywords().Required }, []string{"id"}},
		{"TextKeywords", func(schema *statement.Schema) any { return schema.TextKeywords() },
			func(schema *statement.Schema) any { return schema.TextKeywords().Pattern }, "^a"},
		{"ChoiceKeywords", func(schema *statement.Schema) any { return schema.ChoiceKeywords() },
			func(schema *statement.Schema) any { return schema.ChoiceKeywords().OneOf[0].Name }, "one"},
		{"LogicKeywords", func(schema *statement.Schema) any { return schema.LogicKeywords() },
			func(schema *statement.Schema) any { return schema.LogicKeywords().Dependencies }, enumNode},
		{"Types", func(schema *statement.Schema) any { return schema.Types() },
			func(schema *statement.Schema) any { return schema.Types() }, []statement.JSONSchemaTypeName{statement.KindString}},
		{"Enum", func(schema *statement.Schema) any { return schema.Enum() },
			func(schema *statement.Schema) any { return schema.Enum() }, []*yaml.Node{enumNode}},
		{"Const", func(schema *statement.Schema) any { return schema.Const() },
			func(schema *statement.Schema) any { return schema.Const() }, constNode},
		{"FormatName", func(schema *statement.Schema) any { return schema.FormatName() },
			func(schema *statement.Schema) any { return schema.FormatName() }, "uuid"},
		{"PrefixItems", func(schema *statement.Schema) any { return schema.PrefixItems() },
			func(schema *statement.Schema) any { return schema.PrefixItems()[0].Name }, "first"},
		{"Discriminator", func(schema *statement.Schema) any { return schema.Discriminator() },
			func(schema *statement.Schema) any { return schema.Discriminator().PropertyName }, "kind"},
		{"Default", func(schema *statement.Schema) any { return schema.Default() },
			func(schema *statement.Schema) any { return schema.Default() }, defNode},
		{"XML", func(schema *statement.Schema) any { return schema.XML() },
			func(schema *statement.Schema) any { return *schema.XML().Name }, "tag"},
		{"ExternalDocs", func(schema *statement.Schema) any { return schema.ExternalDocs() },
			func(schema *statement.Schema) any { return schema.ExternalDocs().URL }, "https://example.com"},
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
