// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// d4 is a dialect a $schema names in these tests.
const d4 = "'http://json-schema.org/draft-04/schema#'"

// rootsOf returns where each dialect root of table is, and the URI it names.
func rootsOf(table *bind.Table) map[bind.Position]string {
	out := map[bind.Position]string{}
	for _, r := range dialectRoots(table) {
		out[r.At] = r.URI
	}
	return out
}

// dialectRoots returns the dialect roots of every document table read.
func dialectRoots(table *bind.Table) []bind.DialectRoot {
	var out []bind.DialectRoot
	for id := range bind.DocID(table.Documents()) {
		out = append(out, table.Document(id).Dialects...)
	}
	return out
}

// TestDialectRoots_ASchemaNoOtherHoldsNamesItsDialect pins which `$schema` is a
// root's: one a Schema Object no other holds writes, at a component, a
// parameter or a media type alike, and not one a subschema writes, nor a
// value an example holds, nor a property that happens to be named `$schema`.
// Each root's node is the schema's own.
func TestDialectRoots_ASchemaNoOtherHoldsNamesItsDialect(t *testing.T) {
	t.Parallel()
	table := bound(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"+
		"paths:\n  /p:\n    get:\n      parameters:\n        - {name: q, in: query, schema: {$schema: "+d4+"}}\n"+
		"      responses:\n        '200':\n          description: ok\n          content:\n"+
		"            application/json: {schema: {$schema: "+d4+"}}\n"+
		"components:\n  schemas:\n"+
		"    A: {$schema: "+d4+", properties: {a: {$schema: 'https://example.com/inner'}}}\n"+
		"    B: {example: {$schema: 'https://example.com/example'}}\n"+
		"    C: {properties: {$schema: {type: string}}}\n"+
		"    D: {$schema: null}\n", self)
	d := "http://json-schema.org/draft-04/schema#"
	assert.Equal(t, map[bind.Position]string{
		at("/components/schemas/A"):                                         d,
		at("/paths/~1p/get/parameters/0/schema"):                            d,
		at("/paths/~1p/get/responses/200/content/application~1json/schema"): d,
	}, rootsOf(table))
	for _, r := range dialectRoots(table) {
		require.NotNil(t, r.Node, r.At.Pointer)
		assert.Equal(t, "$schema", r.Node.Content[0].Value, "%s is the schema's node", r.At.Pointer)
	}
}

// TestDialectRoots_ADocumentOfSchemasNamesItAtItsRoot pins that a document no
// OpenAPI Object roots names its dialect at its root, which holds every schema
// in it, so a `$schema` below that names nothing, whatever key holds it.
func TestDialectRoots_ADocumentOfSchemasNamesItAtItsRoot(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		other string
		want  []jsontext.Pointer
	}{
		"at its root": {"{$schema: " + d4 + ", definitions: {E: {type: integer}}}\n", []jsontext.Pointer{""}},
		"below it":    {"E: {$schema: " + d4 + ", type: integer}\n", nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ref := "o.yaml#/definitions/E"
			if tc.want == nil {
				ref = "o.yaml#/E"
			}
			table := bound(t, head+"components:\n  schemas:\n    S: {$ref: '"+ref+"'}\n",
				withDocuments(map[string]string{"o.yaml": tc.other}))
			doc := docNamed(t, table, "o.yaml")
			var got []jsontext.Pointer
			for _, r := range table.Document(doc).Dialects {
				got = append(got, r.At.Pointer)
			}
			assert.Len(t, dialectRoots(table), len(got), "the source names none")
			assert.Equal(t, tc.want, got)
		})
	}
}
