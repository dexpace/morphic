// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// laterCase is a document writing one construct a later minor added.
type laterCase struct {
	row  laterRow
	kind oas.Kind
	doc  string
	key  string // the pointer token of the key that writes it
}

// laterCases write each of the 34 constructs once, in the object that holds it.
func laterCases() []laterCase {
	return []laterCase{
		{laterRows()[0], oas.Document, `{jsonSchemaDialect: "https://a.example/d"}`, "jsonSchemaDialect"},
		{laterRows()[1], oas.Document, `{webhooks: {}}`, "webhooks"},
		{laterRows()[2], oas.Document, `{$self: "https://a.example/s"}`, "$self"},
		{laterRows()[3], oas.Info, `{summary: s}`, "summary"},
		{laterRows()[4], oas.License, `{name: n, identifier: i}`, "identifier"},
		{laterRows()[5], oas.Components, `{pathItems: {}}`, "pathItems"},
		{laterRows()[6], oas.Components, `{mediaTypes: {}}`, "mediaTypes"},
		{laterRows()[7], oas.Server, `{url: u, name: n}`, "name"},
		{laterRows()[8], oas.Tag, `{name: n, summary: s}`, "summary"},
		{laterRows()[9], oas.Tag, `{name: n, parent: p}`, "parent"},
		{laterRows()[10], oas.Tag, `{name: n, kind: k}`, "kind"},
		{laterRows()[11], oas.PathItem, `{query: {}}`, "query"},
		{laterRows()[12], oas.PathItem, `{additionalOperations: {}}`, "additionalOperations"},
		{laterRows()[13], oas.Parameter, `{name: n, in: querystring, content: {a/b: {}}}`, "in"},
		{laterRows()[14], oas.Parameter, `{name: n, in: cookie, style: cookie, schema: {}}`, "style"},
		{laterRows()[15], oas.MediaType, `{$ref: "#/a"}`, "$ref"},
		{laterRows()[16], oas.MediaType, `{description: d}`, "description"},
		{laterRows()[17], oas.MediaType, `{itemSchema: {}}`, "itemSchema"},
		{laterRows()[18], oas.MediaType, `{itemEncoding: {}}`, "itemEncoding"},
		{laterRows()[19], oas.MediaType, `{prefixEncoding: []}`, "prefixEncoding"},
		{laterRows()[20], oas.Encoding, `{encoding: {}}`, "encoding"},
		{laterRows()[21], oas.Encoding, `{prefixEncoding: []}`, "prefixEncoding"},
		{laterRows()[22], oas.Encoding, `{itemEncoding: {}}`, "itemEncoding"},
		{laterRows()[23], oas.Example, `{dataValue: 1}`, "dataValue"},
		{laterRows()[24], oas.Example, `{serializedValue: s}`, "serializedValue"},
		{laterRows()[25], oas.Response, `{description: d, summary: s}`, "summary"},
		{laterRows()[26], oas.SecurityScheme, `{type: mutualTLS}`, "type"},
		{laterRows()[27], oas.SecurityScheme, `{type: oauth2, flows: {}, oauth2MetadataUrl: u}`, "oauth2MetadataUrl"},
		{laterRows()[28], oas.SecurityScheme, `{type: mutualTLS, deprecated: true}`, "deprecated"},
		{laterRows()[29], oas.OAuthFlows, `{deviceAuthorization: {}}`, "deviceAuthorization"},
		{laterRows()[30], oas.Discriminator, `{propertyName: p, defaultMapping: m}`, "defaultMapping"},
		{laterRows()[31], oas.XML, `{nodeType: element}`, "nodeType"},
		{laterRows()[32], oas.Reference, `{$ref: "#/a", summary: s}`, "summary"},
		{laterRows()[33], oas.Reference, `{$ref: "#/a", description: d}`, "description"},
	}
}

// TestCheck_LaterConstructsAreWarnedOfInTheCompilersWords pins the warning each
// of the 34 constructs draws in a document declaring the minor before the one
// that added it: its code, its place, and its wording.
func TestCheck_LaterConstructsAreWarnedOfInTheCompilersWords(t *testing.T) {
	cases := laterCases()
	require.Len(t, cases, len(laterRows()))
	for _, tc := range cases {
		declared := "3.0.3"
		if tc.row.Since == "3.2" {
			declared = "3.1.0"
		}
		what := fmt.Sprintf("field %q", tc.row.Key)
		if tc.row.Value != "" {
			what = fmt.Sprintf("value %q of field %q", tc.row.Value, tc.row.Key)
		}
		want := fmt.Sprintf("%s of the %s was added in OpenAPI %s, and this document declares %s; it is lowered as in an OpenAPI %s document",
			what, tc.row.Object, tc.row.Since, declared, tc.row.Since)
		t.Run(tc.row.Object+"/"+tc.row.Key+"/"+tc.row.Value, func(t *testing.T) {
			var got []ir.Diagnostic
			for _, d := range run(t, declared, tc.kind, tc.doc) {
				if d.Code == diag.LaterVersionConstruct {
					got = append(got, d)
				}
			}
			require.Len(t, got, 1)
			assert.Equal(t, ir.SeverityWarning, got[0].Severity)
			assert.Equal(t, want, got[0].Message)
			assert.Equal(t, "/"+tc.key, string(got[0].Provenance.Pointer))
		})
		t.Run(tc.row.Object+"/"+tc.row.Key+"/"+tc.row.Value+" at the minor that added it", func(t *testing.T) {
			for _, d := range run(t, tc.row.Since+".0", tc.kind, tc.doc) {
				assert.NotEqual(t, diag.LaterVersionConstruct, d.Code, d.Message)
			}
		})
	}
}

// laterKeywordValue is a value of fld's shape and format a 3.1 schema admits.
func laterKeywordValue(fld oas.Field) string {
	switch fld.Shape {
	case oas.String:
		if fld.Format == oas.AnchorName {
			return "a"
		}
		return "'https://e.com/a'"
	case oas.Natural:
		return "1"
	case oas.Nested, oas.Map, oas.RawMap:
		return "{}"
	case oas.List:
		return "[{}]"
	default:
		return "[1]"
	}
}

// TestCheck_AKeywordALaterDialectAddsIsWarnedOf pins each keyword the Schema
// Object defines from 3.1 and not at 3.0, written in a 3.0 schema: warned of
// once, at the key, as a later minor's construct, and judged as 3.1 judges
// it, so a boolean schema stands where 3.1's admits one. A `$schema` there is
// worded as choosing nothing. A key beside a 3.0 $ref draws the warning every
// key there draws, alone.
func TestCheck_AKeywordALaterDialectAddsIsWarnedOf(t *testing.T) {
	obj, ok := oas.Table().Object(oas.Schema)
	require.True(t, ok)
	at30 := map[string]bool{}
	for _, f := range obj.Fields {
		if f.Defined.Holds(oas.V30) {
			at30[f.Name] = true
		}
	}
	var keywords []string
	for _, f := range obj.Fields {
		if f.Defined.Holds(oas.V31) && !at30[f.Name] {
			keywords = append(keywords, f.Name)
			doc := "{" + f.Name + ": " + laterKeywordValue(f) + "}"
			want := fmt.Sprintf("field %q of the Schema Object was added in OpenAPI 3.1, and this document declares "+
				"3.0.3; it is lowered as in an OpenAPI 3.1 document", f.Name)
			if f.Name == "$schema" {
				want = "field \"$schema\" of the Schema Object was added in OpenAPI 3.1, and this document declares " +
					"3.0.3; a 3.0 schema is read in 3.0's own dialect whatever it names, so it is kept verbatim and chooses nothing"
			}
			diags := run(t, "3.0.3", oas.Schema, doc)
			require.Len(t, diags, 1, doc)
			assert.Equal(t, diag.LaterVersionConstruct, diags[0].Code, doc)
			assert.Equal(t, ir.SeverityWarning, diags[0].Severity, doc)
			assert.Equal(t, "/"+f.Name, string(diags[0].Provenance.Pointer), doc)
			assert.Equal(t, want, diags[0].Message, doc)
			assert.Empty(t, run(t, "3.1.0", oas.Schema, doc), "%s at the minor that added it", doc)
		}
	}
	assert.Contains(t, keywords, "prefixItems")
	verify(t, []scenario{
		{"a boolean schema 3.1 admits", "3.0.3", oas.Schema, `{unevaluatedProperties: false, if: true}`,
			[]finding{at(later, "/unevaluatedProperties"), at(later, "/if")}},
		{"a boolean schema 3.1 admits as an entry", "3.0.3", oas.Schema, `{prefixItems: [true], $defs: {A: false}}`,
			[]finding{at(later, "/prefixItems"), at(later, "/$defs")}},
		{"a value 3.1 refuses", "3.0.3", oas.Schema, `{const: 1, contentSchema: 5}`,
			[]finding{at(later, "/const"), at(later, "/contentSchema"), at(mismatch, "/contentSchema")}},
		{"beside a $ref", "3.0.3", oas.Schema, `{$ref: '#/a', const: 1, prefixItems: [{}]}`,
			[]finding{at(later, "/const"), at(later, "/prefixItems")}},
		{"unknown keys are not reported", "3.1.0", oas.Schema, `{madeUp: 1, x-ext: 2}`, nil},
	})
	for _, d := range run(t, "3.0.3", oas.Schema, `{$ref: '#/a', const: 1}`) {
		assert.Contains(t, d.Message, "beside the $ref", "the sibling's warning, not the keyword's")
	}
}

// schemaRefSiblings writes keys beside a schema's $ref at each kind of position
// a schema stands at, one $ref with nothing beside it, and a $ref beside which
// a schema is written that has a $ref of its own.
const schemaRefSiblings = `info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/S', default: x}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/S'}
components:
  schemas:
    S: {type: string}
    Alias: {$ref: '#/components/schemas/S', description: d, x-note: n}
    M:
      type: object
      properties:
        p: {$ref: '#/components/schemas/S', nullable: true, readOnly: true}
        l: {type: array, items: {$ref: '#/components/schemas/S', type: string}}
        c: {allOf: [{$ref: '#/components/schemas/S', title: t}]}
        o:
          $ref: '#/components/schemas/S'
          properties:
            z: {$ref: '#/components/schemas/S', minLength: 1}
`

// TestCheck_EveryKeyBesideA30SchemaRefIsALaterConstruct pins GitHub #871: in
// 3.0 a schema written as a $ref is a Reference Object, which ignores every key
// beside the $ref, while 3.1 applies them as the schema's keywords. The
// lowering applies them as 3.1 does, each read as 3.0 defines it, so below 3.1
// each key beside the $ref is warned of at its own pointer, whatever the
// position and the key, and the $ref itself never is.
func TestCheck_EveryKeyBesideA30SchemaRefIsALaterConstruct(t *testing.T) {
	laterAt := func(version string) map[string]string {
		got := map[string]string{}
		for _, d := range run(t, version, oas.Document, "openapi: "+version+"\n"+schemaRefSiblings) {
			if d.Code == diag.LaterVersionConstruct {
				assert.Equal(t, ir.SeverityWarning, d.Severity)
				got[string(d.Provenance.Pointer)] = d.Message
			}
		}
		return got
	}
	got := laterAt("3.0.3")
	assert.ElementsMatch(t, []string{
		"/components/schemas/Alias/description",
		"/components/schemas/Alias/x-note",
		"/components/schemas/M/properties/c/allOf/0/title",
		"/components/schemas/M/properties/l/items/type",
		"/components/schemas/M/properties/o/properties",
		"/components/schemas/M/properties/o/properties/z/minLength",
		"/components/schemas/M/properties/p/nullable",
		"/components/schemas/M/properties/p/readOnly",
		"/paths/~1a/get/parameters/0/schema/default",
	}, slices.Collect(maps.Keys(got)))
	assert.Equal(t, `keyword "readOnly" beside the $ref of a Schema Object is ignored by OpenAPI 3.0 and applied by `+
		`OpenAPI 3.1, and this document declares 3.0.3; it is applied as in an OpenAPI 3.1 document `+
		`and read as in an OpenAPI 3.0 one`,
		got["/components/schemas/M/properties/p/readOnly"])
	for _, version := range []string{"3.1.0", "3.2.0"} {
		assert.Empty(t, laterAt(version), version)
	}
}
