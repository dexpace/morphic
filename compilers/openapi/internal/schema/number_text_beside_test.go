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

// TestScalar_ABoundBesideANumberAsTextPrincipalIsKeptAsItsOwn pins the value
// bounds written beside an allOf whose inline principal carries a number as
// text: each kept on the Scalar under no_ir_home, as one written beside the
// principal's own keywords is, and a keyword no bound, a count of items,
// kept as any keyword beside a refinement is, degraded_lowering.
func TestScalar_ABoundBesideANumberAsTextPrincipalIsKeptAsItsOwn(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    B: {allOf: [{type: string, format: double}], minimum: 31, pattern: '^[0-9]+$', minItems: 2}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	sc, ok := typeByName(doc, "B").(*ir.Scalar)
	require.True(t, ok, "the principal's Scalar stands at the position")
	assert.Nil(t, sc.Constraints)
	for keyword, want := range map[string]ir.UnmodeledReason{
		"openapi:minimum": ir.ReasonNoIRHome, "openapi:pattern": ir.ReasonNoIRHome, "openapi:minItems": ir.ReasonDegradedLowering,
	} {
		entry, kept := sc.Unmodeled[keyword]
		if assert.True(t, kept, "%s is kept", keyword) {
			assert.Equal(t, want, entry.Reason, keyword)
		}
	}
}
