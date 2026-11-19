// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// apartCase is a schema whose inline allOf principal and position may both
// keep a keyword, at the type id its position lowers to: what the node holds
// under each plain key, under the principal's entry key where one is kept,
// and the pointer the principal's report is drawn at, "" for none.
type apartCase struct {
	name, schema string
	id           ir.TypeID
	plain        map[string]string
	entryKey     string
	entry        map[string]any
	reportedAt   jsontext.Pointer
}

// apartCases write a keyword both an inline principal and its position keep.
var apartCases = []apartCase{
	{"one keyword", "X1: {allOf: [{type: string, minimum: 1}], minimum: 2}", componentID("X1"),
		map[string]string{"minimum": "2"}, "openapi:allOf/0", map[string]any{"minimum": 1.0}, "/components/schemas/X1/allOf/0"},
	{"a number as text", "X2: {allOf: [{type: string, format: double, minimum: 1}], minimum: 2}", componentID("X2"),
		map[string]string{"minimum": "2"}, "openapi:allOf/0", map[string]any{"minimum": 1.0}, "/components/schemas/X2/allOf/0"},
	{"two keywords", "X3: {allOf: [{type: string, minimum: 1, maximum: 5}], minimum: 2, maximum: 6}", componentID("X3"),
		map[string]string{"minimum": "2", "maximum": "6"}, "openapi:allOf/0", map[string]any{"minimum": 1.0, "maximum": 5.0},
		"/components/schemas/X3/allOf/0"},
	{"one of two keywords", "X4: {allOf: [{type: string, minimum: 1, maximum: 5}], minimum: 2}", componentID("X4"),
		map[string]string{"minimum": "2", "maximum": "5"}, "openapi:allOf/0", map[string]any{"minimum": 1.0}, "/components/schemas/X4/allOf/0"},
	{"another entry after", "X5: {allOf: [{type: string, minimum: 1}, {description: d}], minimum: 2}", componentID("X5"),
		map[string]string{"minimum": "2"}, "openapi:allOf/0", map[string]any{"minimum": 1.0}, "/components/schemas/X5/allOf/0"},
	{"another entry before", "X6: {allOf: [{description: d}, {type: string, minimum: 1}], minimum: 2}", componentID("X6"),
		map[string]string{"minimum": "2"}, "openapi:allOf/1", map[string]any{"minimum": 1.0}, "/components/schemas/X6/allOf/1"},
	{"a list", "X7: {allOf: [{type: array, items: {type: string}, minLength: 1}], minLength: 2}", componentID("X7"),
		map[string]string{"minLength": "2"}, "openapi:allOf/0", map[string]any{"minLength": 1.0}, "/components/schemas/X7/allOf/0"},
	{"alike", "X8: {allOf: [{type: string, minimum: 2}], minimum: 2}", componentID("X8"),
		map[string]string{"minimum": "2"}, "", nil, ""},
	{"a $ref principal", "X9: {allOf: [{$ref: '#/components/schemas/S'}], minimum: 2}", componentID("X9"),
		map[string]string{"minimum": "2"}, "", nil, ""},
	{"a property", "P: {type: object, properties: {p: {allOf: [{type: string, minimum: 1}], minimum: 2}}}",
		"t/anon/components/schemas/P/properties/p", map[string]string{"minimum": "2"}, "openapi:allOf/0",
		map[string]any{"minimum": 1.0}, "/components/schemas/P/properties/p/allOf/0"},
	{"an array's items", "I: {type: array, items: {allOf: [{type: string, minimum: 1}], minimum: 2}}",
		"t/anon/components/schemas/I/items", map[string]string{"minimum": "2"}, "openapi:allOf/0",
		map[string]any{"minimum": 1.0}, "/components/schemas/I/items/allOf/0"},
	{"a draft's exclusive flag", "D: {$id: 'https://example.com/d', $schema: " + draft04 +
		", allOf: [{type: string, minimum: 1, exclusiveMinimum: true}], minimum: 2, exclusiveMinimum: false}", componentID("D"),
		map[string]string{"minimum": "2", "exclusiveMinimum": "false"}, "openapi:allOf/0",
		map[string]any{"minimum": 1.0, "exclusiveMinimum": true}, "/components/schemas/D/allOf/0"},
}

// TestPrincipalApart_AKeywordThePrincipalAndThePositionKeepHoldsBoth pins a
// keyword an inline allOf principal and its position both keep verbatim, written
// otherwise, in both declaration orders: the plain key holds the position's,
// as the flat spelling's does, and the principal's is kept on the same node
// under its entry's key, holding those keywords alone, with one report at the
// entry. Written alike, or beside a $ref principal, whose target is its own
// node, it is kept once (docs/compilers/openapi.md#allof-inline-residue).
func TestPrincipalApart_AKeywordThePrincipalAndThePositionKeepHoldsBoth(t *testing.T) {
	t.Parallel()
	lines := make([]string, 0, 1+len(apartCases))
	lines = append(lines, "    S: {type: string, minimum: 1}")
	for _, tc := range apartCases {
		lines = append(lines, "    "+tc.schema)
	}
	reversed := slices.Clone(lines)
	slices.Reverse(reversed)
	for _, order := range [][]string{lines, reversed} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(strings.Join(order, "\n")+"\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, tc := range apartCases {
			t.Run(tc.name, func(t *testing.T) {
				td, ok := doc.Types[tc.id]
				require.True(t, ok, "%s is a node", tc.id)
				held := td.Common().Unmodeled
				for key, want := range tc.plain {
					assert.Equal(t, want, string(unmodeledEntry(t, held, "openapi:"+key).Value), "the plain key holds the position's %s", key)
				}
				var entries []string
				for key := range held {
					if strings.HasPrefix(key, "openapi:allOf/") {
						entries = append(entries, key)
					}
				}
				if tc.entryKey == "" {
					assert.Empty(t, entries, "nothing is kept under an entry")
				} else {
					assert.Equal(t, []string{tc.entryKey}, entries)
					var got map[string]any
					require.NoError(t, json.Unmarshal(held[tc.entryKey].Value, &got))
					assert.Equal(t, tc.entry, got, "the entry's key holds the principal's colliding keywords alone")
					assert.Equal(t, tc.reportedAt, held[tc.entryKey].Provenance.Pointer, "kept where the principal writes it")
				}
				var said []string
				for _, d := range diags {
					if d.Code != diag.DegradedConstruct || d.Provenance.Pointer != tc.reportedAt {
						continue
					}
					if strings.Contains(d.Message, "otherwise than this inline entry") {
						said = append(said, d.Message)
					}
					if _, listed, ok := strings.Cut(d.Message, "which has no home for "); ok {
						for key := range tc.entry {
							assert.NotContains(t, strings.Fields(strings.ReplaceAll(listed, ",", "")), key,
								"the principal's %s draws no second report", key)
						}
					}
				}
				want := 0
				if tc.reportedAt != "" {
					want = 1
				}
				assert.Len(t, said, want, "one report at the entry")
			})
		}
	}
}

// TestPrincipalApart_AParameterSchemaHoldsBoth pins the rule at a parameter's
// schema, whose node stands for the parameter.
func TestPrincipalApart_AParameterSchemaHoldsBoth(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /a:\n    get:\n"+
		"      parameters:\n        - {name: q, in: query, schema: {allOf: [{type: string, minimum: 1}], minimum: 2}}\n"+
		"      responses: {'204': {description: ok}}\n")
	openapitest.RequireNoErrorDiags(t, diags)
	held := doc.Types["t/anon/paths/~1a/get/parameters/0/schema"].Common().Unmodeled
	assert.Equal(t, "2", string(unmodeledEntry(t, held, "openapi:minimum").Value))
	assert.JSONEq(t, `{"minimum":1}`, string(unmodeledEntry(t, held, "openapi:allOf/0").Value))
}

// TestPrincipalApart_AReportNamesTheEntryWhereACarrierKeepsIt pins the report of
// a principal's keyword kept under its entry's key where the position owns no
// node: the parameter keeps it beneath its schema's scope, so the report names
// the entry's path, which that key ends in, and no key the parameter lacks.
func TestPrincipalApart_AReportNamesTheEntryWhereACarrierKeepsIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /a:\n    get:\n"+
		"      parameters:\n        - {name: q, in: query, schema: {allOf: [{type: integer, contentEncoding: a}], contentEncoding: b}}\n"+
		"      responses: {'204': {description: ok}}\n")
	openapitest.RequireNoErrorDiags(t, diags)
	raw, err := json.Marshal(doc, json.Deterministic(true))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"openapi:schema/allOf/0":`)
	assert.NotContains(t, string(raw), `"openapi:allOf/0":`)
	var said []string
	for _, d := range diags {
		if d.Provenance.Pointer == "/paths/~1a/get/parameters/0/schema/allOf/0" {
			said = append(said, d.Message)
		}
	}
	require.Len(t, said, 1)
	assert.Contains(t, said[0], "under the key naming it, allOf/0,")
	assert.NotContains(t, said[0], "openapi:allOf/0")
}

// TestPrincipalApart_AKeptDiscriminatorResolvesItsMapping pins a principal's
// discriminator kept under its entry's key, apart from the position's or held
// in the whole entry, in both declaration orders: its mapping is resolved as a
// discriminator kept unread is, so a value naming no schema draws its one
// error at the mapping entry (docs/compilers/openapi.md#allof-inline-residue).
func TestPrincipalApart_AKeptDiscriminatorResolvesItsMapping(t *testing.T) {
	t.Parallel()
	lines := []string{
		"    Apart: {allOf: [{type: string, discriminator: {propertyName: a, mapping: {x: '#/components/schemas/Nope'}}}], " +
			"discriminator: {propertyName: b}}",
		"    Held: {allOf: [{type: string, x-k: 1, discriminator: {propertyName: a, mapping: {x: '#/components/schemas/Nope'}}}], " +
			"x-k: 2}",
	}
	reversed := slices.Clone(lines)
	slices.Reverse(reversed)
	for _, order := range [][]string{lines, reversed} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(strings.Join(order, "\n")+"\n"))
		for _, name := range []string{"Apart", "Held"} {
			held := doc.Types[ir.TypeID("t/openapi/components/schemas/"+name)].Common().Unmodeled
			assert.Contains(t, string(unmodeledEntry(t, held, "openapi:allOf/0").Value), "#/components/schemas/Nope", name)
			at := jsontext.Pointer("/components/schemas/" + name + "/allOf/0/discriminator/mapping/x")
			errs := slices.DeleteFunc(slices.Clone(diags), func(d ir.Diagnostic) bool {
				return d.Code != diag.UnresolvedRef || d.Provenance.Pointer != at
			})
			assert.Len(t, errs, 1, "%s: the mapping naming no schema is reported once at %s", name, at)
		}
	}
}
