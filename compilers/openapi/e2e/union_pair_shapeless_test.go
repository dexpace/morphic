// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestElection_TwoShapelessUnionsSharingNoValueWarnBesideTheirBody holds that
// a oneOf and an anyOf sharing no value, neither declaring shape against the
// body beside them, draw the one warning that the conjuncts share no value
// where they are written beside the body's own keywords, as beside an allOf of
// the body and on its entries (GitHub #2011).
func TestElection_TwoShapelessUnionsSharingNoValueWarnBesideTheirBody(t *testing.T) {
	t.Parallel()
	cases := []struct{ body, oneOf, anyOf string }{
		{"{type: string}", "[{type: 'null'}]", "[{type: string}]"},
		{"{type: object, properties: {a: {type: string}}}", "[{type: 'null'}]", "[{type: object}]"},
	}
	for _, tc := range cases {
		spellings := unionPairSpellings(tc.body, tc.oneOf, tc.anyOf)
		spellings["beside the body"] = strings.TrimSuffix(tc.body, "}") + ", oneOf: " + tc.oneOf + ", anyOf: " + tc.anyOf + "}"
		for name, schema := range spellings {
			for _, reversed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", tc.body, name, reversed), func(t *testing.T) {
					t.Parallel()
					_, diags := compileAnnotationSpec(t, "shapeless", bothKeywordsSites[0].doc("3.1.0", schema, reversed))
					requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/P",
						contains: []string{"share no value", "so no instance satisfies this schema"}}})
				})
			}
		}
	}
}

// TestElection_TwoShapelessUnionsSharingNullBesideABodyAreNotNamedApart holds
// that a oneOf and an anyOf sharing null, written beside a body admitting no
// null, are not reported as sharing no value with each other: they share the
// null, which the body keeps out.
func TestElection_TwoShapelessUnionsSharingNullBesideABodyAreNotNamedApart(t *testing.T) {
	t.Parallel()
	_, diags := compileAnnotationSpec(t, "shared", bothKeywordsSites[0].doc("3.1.0",
		"{type: string, oneOf: [{type: 'null'}], anyOf: [{type: 'null'}]}", false))
	for _, diagnostic := range diags {
		assert.NotContains(t, diagnostic.Message, "(oneOf: null, anyOf: null)")
	}
}
