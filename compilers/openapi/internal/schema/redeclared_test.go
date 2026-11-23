// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestRefUseRedeclared_ANoteGivenApartKeepsTheNode pins which of two uses of
// Info, each writing a note beside its $ref, that one model writes itself for
// one member keeps the node its schema builds: each where the two give one
// note otherwise, a value that does not convert among them, and neither where
// they agree, write different notes, or are no declarations of one model's
// member, the field then naming Info (docs/compilers/openapi.md#ref-use-site).
func TestRefUseRedeclared_ANoteGivenApartKeepsTheNode(t *testing.T) {
	t.Parallel()
	const info = "    Info: {type: object, properties: {count: {type: integer}}}\n"
	use := func(note string) string { return "{$ref: '#/components/schemas/Info', " + note + "}" }
	twoEntries := func(a, b string) string {
		return info + "    Coll: {allOf: [{properties: {f: " + use(a) + "}}, {properties: {f: " + use(b) + "}}]}\n"
	}
	const entry0, entry1 = "Coll/allOf/0/properties/f", "Coll/allOf/1/properties/f"
	for _, tc := range []struct {
		name, schemas string
		pointers      []string
		kept          bool
	}{
		{"descriptions apart", twoEntries("description: a", "description: b"), []string{entry0, entry1}, true},
		{"examples apart", twoEntries("example: {count: 1}", "example: {count: 2}"), []string{entry0, entry1}, true},
		{"values that do not convert", twoEntries("example: !acme x", "example: !acme x"), []string{entry0, entry1}, true},
		{"one description", twoEntries("description: a", "description: a"), []string{entry0, entry1}, false},
		{"a description and a title", twoEntries("description: a", "title: b"), []string{entry0, entry1}, false},
		{"beside the model's own declaration", info + "    Coll: {properties: {f: " + use("description: a") + "}, " +
			"allOf: [{properties: {f: " + use("description: b") + "}}]}\n", []string{"Coll/properties/f", "Coll/allOf/0/properties/f"}, true},
		{"one declaration of a value that does not convert", info + "    Coll: {type: object, properties: {f: " +
			use("example: !acme x") + "}}\n", []string{"Coll/properties/f"}, false},
		{"under a property named allOf", info + "    Coll: {type: object, properties: {allOf: {type: array, " +
			"items: {type: object, properties: {f: " + use("description: a") + "}}}}}\n", []string{"Coll/properties/allOf/items/properties/f"}, false},
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
		openapitest.RequireNoErrorDiags(t, diags)
		require.NotNil(t, typeByName(doc, "Coll"), tc.name)

		for _, pointer := range tc.pointers {
			node, kept := doc.Types[ir.TypeID("type/anonymous/components/schemas/"+pointer)].(*ir.Model)
			assert.Equal(t, tc.kept, kept, "%s: whether the use at %s keeps its node", tc.name, pointer)
			if kept {
				require.NotNil(t, node.Base, tc.name)
				assert.Equal(t, componentID("Info"), node.Base.Target, "%s: the node at %s composes Info", tc.name, pointer)
			}
		}
	}
}

// TestRefUseRedeclared_AParameterReadsNoMember pins that a parameter's schema
// writing a note beside its $ref is the reference whatever a model declares:
// no member of a model is a parameter's schema.
func TestRefUseRedeclared_AParameterReadsNoMember(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p:
    get:
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/Info', description: a}}
      responses: {"200": {description: ok}}
components:
  schemas:
    Info: {type: object, properties: {count: {type: integer}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	for id, td := range doc.Types {
		model, ok := td.(*ir.Model)
		assert.False(t, ok && model.Anonymous && model.Base != nil && model.Base.Target == componentID("Info"),
			"%s: no node composes Info, as the parameter names it", id)
	}
}
