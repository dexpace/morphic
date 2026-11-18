// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// unsatisfiedPairs are a oneOf and an anyOf over an object body that no
// instance satisfies together, each union written as its branches.
var unsatisfiedPairs = []struct{ name, oneOf, anyOf string }{
	{"no oneOf branch admits an object beside an anyOf of objects", "[{type: string}, {type: array}]",
		"[{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]"},
	{"no anyOf branch admits an object beside a oneOf of objects", bothKeywordsOneOf, "[{type: string}, {type: array}]"},
	{"neither admits an object", "[{type: string}, {type: integer}]", "[{type: string}, {type: array}]"},
	{"an anyOf admitting no object beside a oneOf declaring no shape", "[{required: [a]}, {maxProperties: 3}]",
		"[{type: string}, {type: integer}]"},
	{"an anyOf admitting no object beside a oneOf restating the body", "[{type: object}]",
		"[{type: string}, {type: array}]"},
	{"a oneOf admitting no object beside an anyOf declaring no shape", "[{type: string}, {type: integer}]",
		"[{required: [a]}, {}]"},
	{"a oneOf with an object branch beside an anyOf admitting no object", "[{type: object, properties: {b: {type: string}}}, {type: string}]",
		"[{type: string}, {type: integer}]"},
}

// TestElection_TwoUnionsNoInstanceSatisfiesLowerAlikeWhereverWritten holds that
// a oneOf and an anyOf no instance of the body satisfies together lower alike
// beside the body, beside its allOf, on one entry and on two, in either order of
// the entries, the keywords and the components, at every version and position,
// over an inline body and a $ref: the position is the Model the body gives,
// keeping both unions as degraded_lowering, and draws one warning that no
// instance satisfies it, whichever union is elected (GitHub #2011).
func TestElection_TwoUnionsNoInstanceSatisfiesLowerAlikeWhereverWritten(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range bothKeywordsSites {
			for _, pair := range unsatisfiedPairs {
				for _, body := range []string{"{type: object, properties: {a: {type: string}}}", entryRef} {
					t.Run(fmt.Sprintf("%s/%s/%s/%t", version, site.name, pair.name, body == entryRef), func(t *testing.T) {
						t.Parallel()
						checkUnsatisfiedPair(t, site, version, unionPairSpellings(body, pair.oneOf, pair.anyOf))
					})
				}
			}
		}
	}
}

// checkUnsatisfiedPair compiles each spelling at site, its components in both
// orders, holds every document to one (compileSpellingsAlike) and that one to
// the Model keeping both unions with one warning at the position.
func checkUnsatisfiedPair(t *testing.T, site bothKeywordsSite, version string, spellings map[string]string) {
	t.Helper()
	doc, diags := compileSpellingsAlike(t, site, version, spellings)
	requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: string(site.at),
		contains: []string{"so no instance satisfies this schema"}}})
	m, ok := kindNode(t, doc, entryPositionID(site.at)).(*ir.Model)
	require.True(t, ok, "the position is the Model its body gives")
	assert.Equal(t, map[string]ir.UnmodeledReason{
		"openapi:oneOf": ir.ReasonDegradedLowering, "openapi:anyOf": ir.ReasonDegradedLowering,
	}, keptOn(m), "it keeps both unions")
}

// infosOnce is the findings of an election view with each info listed once.
func infosOnce(findings any) []string {
	list, _ := findings.([]string)
	var out []string
	for _, f := range list {
		if !strings.HasPrefix(f, string(ir.SeverityInfo)+" ") || !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}
