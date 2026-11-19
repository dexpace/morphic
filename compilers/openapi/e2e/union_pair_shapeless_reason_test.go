// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestElection_TwoShapelessUnionsBesideAScalarKeepBesidesReasonOnEntries holds
// that a oneOf of null alone and an anyOf restating a scalar body, neither
// declaring shape against it and sharing no value, are kept as validation
// logic on the allOf's entries, as beside the allOf, in either order
// (GitHub #2011).
func TestElection_TwoShapelessUnionsBesideAScalarKeepBesidesReasonOnEntries(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"{type: string}", "{type: integer}"} {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", body, reversed), func(t *testing.T) {
				t.Parallel()
				site := bothKeywordsSites[0]
				doc, _ := compileSpellingsAlike(t, site, "3.1.0", unionPairSpellings(body, "[{type: 'null'}]", "["+body+"]"))
				assert.Equal(t, map[string]ir.UnmodeledReason{
					"openapi:oneOf": ir.ReasonValidationOnly, "openapi:anyOf": ir.ReasonValidationOnly,
				}, keptAtUse(t, doc, site), "the position keeps both unions as validation logic")
			})
		}
	}
}
