// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestElection_TwoUnionsSharingNoValueWarnOnceBesideAnyBody holds that a
// oneOf and an anyOf sharing no value, a null shared only where both admit
// it, lower alike beside a string, a string or null, an array, an object or
// null and an object, on one schema, beside an allOf, on one entry or two, in
// either order: as the schema writing them beside its body lowers, with the
// one warning that the conjuncts share no value (GitHub #2011).
func TestElection_TwoUnionsSharingNoValueWarnOnceBesideAnyBody(t *testing.T) {
	t.Parallel()
	bodies := []string{"{type: string}", "{type: [string, 'null']}", "{type: array, items: {type: string}}",
		"{type: [object, 'null'], properties: {a: {type: string}}}", "{type: object, properties: {a: {type: string}}}"}
	pairs := []struct{ name, oneOf, anyOf string }{
		{"a string beside an object or null", "[{type: string}]", "[{type: object, properties: {x: {type: string}}}, {type: 'null'}]"},
		{"a string beside a boolean", "[{type: string}]", "[{type: boolean}]"},
		{"objects beside an array", "[{type: object, properties: {x: {type: string}}}, {type: object, properties: {y: {type: string}}}]",
			"[{type: array}]"},
	}
	for _, body := range bodies {
		for _, pair := range pairs {
			for _, reversed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", body, pair.name, reversed), func(t *testing.T) {
					t.Parallel()
					one, anyOf := pair.oneOf, pair.anyOf
					if reversed {
						one, anyOf = reversedBranches(t, one), reversedBranches(t, anyOf)
					}
					spellings := unionPairSpellings(body, one, anyOf)
					if !strings.HasPrefix(body, "{type: [") {
						// A type list beside one union lowers apart from the same on an
						// entry already, which the pair inherits.
						spellings["beside the body"] = strings.TrimSuffix(body, "}") + ", oneOf: " + one + ", anyOf: " + anyOf + "}"
					}
					_, diags := compileSpellingsAlike(t, bothKeywordsSites[0], "3.1.0", spellings)
					requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/P",
						contains: []string{"share no value", "so no instance satisfies this schema"}}})
				})
			}
		}
	}
}
