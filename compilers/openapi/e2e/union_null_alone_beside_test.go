// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// nullRef is a branch naming a schema of null alone.
const nullRef = "{$ref: '#/components/schemas/Null'}"

// nullAloneSites are a component and a property, each document declaring
// the schema of null alone nullRef names beside T.
var nullAloneSites = []bothKeywordsSite{
	{"component", "/components/schemas/P", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "Null: {type: 'null'}", "P: "+schema)
	}},
	{"property", "/components/schemas/C/properties/p", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "Null: {type: 'null'}", "C: {type: object, properties: {p: "+schema+"}}")
	}},
}

// nullAlonePairs are a oneOf and an anyOf over an object body that share
// no value, the null of one or of both being all they admit beside what the
// body does not.
var nullAlonePairs = []struct{ name, oneOf, anyOf string }{
	{"an object or null beside a string or null", "[{type: object}, {type: 'null'}]", "[{type: string}, {type: 'null'}]"},
	{"a string or null beside objects or null", "[{type: string}, {type: 'null'}]",
		"[{type: object, properties: {x: {type: string}}}, " + nullRef + "]"},
	{"an array or a named null beside an object or null", "[{type: array}, " + nullRef + "]", "[{type: object}, {type: 'null'}]"},
	{"a named null beside a union declaring no shape", "[" + nullRef + "]", "[{required: [a]}]"},
	{"null and a named null beside a union admitting all", "[{type: 'null'}, " + nullRef + "]", "[{}]"},
	{"a union declaring no shape beside a named null", "[{required: [a]}]", "[" + nullRef + "]"},
}

// TestElection_UnionsSharingNullAloneWithEachOtherDrawOneWarning holds that a
// oneOf and an anyOf over an object body sharing no value with it, null
// being all either admits beside the kinds the other or the body lacks, lower
// alike beside the body, beside its allOf, on one entry and two, in either
// order of the entries, the branches and the components: the Model the body
// gives keeping both unions, with one warning that no instance satisfies it,
// as a schema draws one of the two at most (GitHub #2011).
func TestElection_UnionsSharingNullAloneWithEachOtherDrawOneWarning(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.1.0", "3.2.0"} {
		for _, site := range nullAloneSites {
			for _, pair := range nullAlonePairs {
				for _, body := range []string{"{type: object, properties: {a: {type: string}}}", entryRef} {
					for _, reversed := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/%t/%t", version, site.name, pair.name, body == entryRef, reversed), func(t *testing.T) {
							t.Parallel()
							one, anyOf := pair.oneOf, pair.anyOf
							if reversed {
								one, anyOf = reversedBranches(t, one), reversedBranches(t, anyOf)
							}
							checkUnsatisfiedPair(t, site, version, unionPairSpellings(body, one, anyOf))
						})
					}
				}
			}
		}
	}
}

// TestElection_AUnionOfNullAloneBesideAnAllOfDrawsOneWarning holds that a
// union whose every branch admits null alone, one of them naming a schema of
// null, lowers alike beside an allOf of an object body, on an entry of it,
// before the body or after it, one allOf deeper and beside the body's own
// keywords: the Model the body gives keeping the union as degraded_lowering,
// with one warning naming it.
func TestElection_AUnionOfNullAloneBesideAnAllOfDrawsOneWarning(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.1.0", "3.2.0"} {
		for _, site := range nullAloneSites {
			for _, keyword := range []string{"oneOf", "anyOf"} {
				for _, union := range []string{"[" + nullRef + "]", "[{type: 'null'}, " + nullRef + "]", "[" + nullRef + ", {type: 'null'}]"} {
					for _, body := range []string{"{type: object, properties: {a: {type: string}}}", entryRef} {
						t.Run(fmt.Sprintf("%s/%s/%s/%s/%t", version, site.name, keyword, union, body == entryRef), func(t *testing.T) {
							t.Parallel()
							requireNullAloneUnion(t, site, version, keyword, keyword+": "+union, body)
						})
					}
				}
			}
		}
	}
}

// requireNullAloneUnion compiles the union u, written under keyword beside
// body, at site in each spelling and holds them to one Model keeping it, with
// one warning.
func requireNullAloneUnion(t *testing.T, site bothKeywordsSite, version, keyword, u, body string) {
	t.Helper()
	spellings := map[string]string{
		"one entry":        "{allOf: [" + body + ", {" + u + "}]}",
		"before the body":  "{allOf: [{" + u + "}, " + body + "]}",
		"beside the allOf": "{allOf: [" + body + "], " + u + "}",
		"one allOf deeper": "{allOf: [" + body + ", {allOf: [{" + u + "}]}]}",
	}
	if inline, ok := strings.CutPrefix(body, "{type: object, "); ok {
		spellings["beside the keywords"] = "{type: object, " + u + ", " + inline
	}
	doc, diags := compileSpellingsAlike(t, site, version, spellings)
	requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: string(site.at),
		contains: []string{"share no value", "so no instance satisfies this schema"}}})
	m, ok := kindNode(t, doc, entryPositionID(site.at)).(*ir.Model)
	require.True(t, ok, "the position is the Model its body gives")
	assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:" + keyword: ir.ReasonDegradedLowering}, keptOn(m))
}
