// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// fourSpellings are the four ways a schema writes keywords k beside a $ref to
// T: beside it, beside an allOf of it, on its allOf entry, and on an entry of
// their own.
func fourSpellings(k string) map[string]string {
	ref := "$ref: '#/components/schemas/T'"
	return map[string]string{
		"beside the $ref":  "{" + ref + ", " + k + "}",
		"beside an allOf":  "{allOf: [{" + ref + "}], " + k + "}",
		"on its entry":     "{allOf: [{" + ref + ", " + k + "}]}",
		"on an entry next": "{allOf: [{" + ref + "}, {" + k + "}]}",
	}
}

// requireOneDocument holds the four spellings of k beside a $ref to the
// target, at each of the sites given, to one document up to the entries'
// pointers.
func requireOneDocument(t *testing.T, sites []conjSite, target, k string) {
	t.Helper()
	spellings := fourSpellings(k)
	for _, site := range sites {
		want := writtenAnyway(t, site, target, spellings["beside the $ref"])
		for name, schema := range spellings {
			got := writtenAnyway(t, site, target, schema)
			assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(entryWritten{})), "%s, %s", site.name, name)
		}
	}
}

// disjointSets are value keywords that leave their conjunction with a $ref to
// T no value, each beside the target it is written over.
var disjointSets = map[string]struct{ target, keywords string }{
	"a const and an enum sharing no value": {"T: {type: string}", "const: z, enum: [a]"},
	"a const outside its type":             {"T: {type: string}", "type: integer, const: z"},
	"an enum outside its type":             {"T: {type: string}", "type: integer, enum: [a]"},
	"two enums sharing no member":          {"T: {type: string, enum: [a]}", "enum: [b]"},
	"a const and an enum over an object":   {"T: {type: object}", "const: z, enum: [a]"},
	"a const and an enum over a union":     {"T: {oneOf: [{type: string}, {type: integer}]}", "const: 1, enum: [a]"},
	"a const beside a union of another kind": {"T: {oneOf: [{type: string}, {type: integer}]}",
		"const: 1, oneOf: [{type: string}, {type: boolean}]"},
	"an enum beside a union of another kind": {"T: {oneOf: [{type: string}, {type: integer}]}",
		"enum: [1], oneOf: [{type: string}, {type: boolean}]"},
}

// TestRefConjunction_AnEntrysValueSetLowersAsBesideItsRef holds the four
// spellings of value keywords leaving the conjunction no value beside a $ref
// to one document at every site: the value set the dispatch among competing
// keywords picks, never a Model, the rest kept and reported alike.
func TestRefConjunction_AnEntrysValueSetLowersAsBesideItsRef(t *testing.T) {
	t.Parallel()
	for name, tc := range disjointSets {
		requireOneDocument(t, conjSites, tc.target, tc.keywords)
		doc, _ := compileUnrefused(t, conjSites[0].write("3.1.0", fourSpellings(tc.keywords)["on an entry next"], tc.target))
		switch node := kindNode(t, doc, componentID("Use")).(type) {
		case *ir.Literal, *ir.Enum:
		default:
			t.Errorf("%s: the entry lowers to %T, not the value set elected", name, node)
		}
	}
}

// TestRefConjunction_AnEntrysValueSetLowersAlikeInEitherOrder holds an allOf
// writing such value keywords on the entry before its $ref and after it to
// one document at every site.
func TestRefConjunction_AnEntrysValueSetLowersAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/T'}"
	for name, tc := range disjointSets {
		for _, site := range conjSites {
			first := writtenAnyway(t, site, tc.target, "{allOf: [{"+tc.keywords+"}, "+ref+"]}")
			last := writtenAnyway(t, site, tc.target, "{allOf: ["+ref+", {"+tc.keywords+"}]}")
			assert.Empty(t, cmp.Diff(first, last, cmp.AllowUnexported(entryWritten{})), "%s, %s", site.name, name)
		}
	}
}
