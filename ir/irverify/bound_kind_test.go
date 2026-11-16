// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// boundViolations returns the violations of ir/bound-of-another-kind in doc.
func boundViolations(doc *ir.Document) []irverify.Violation {
	var found []irverify.Violation
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/bound-of-another-kind" {
			found = append(found, v)
		}
	}
	return found
}

// TestVerify_AScalarOrUnionHoldingAnyBoundIsNotChecked holds that the check
// stops at structural nodes: a scalar's bounds turn on its encoding and a
// union's on its variants, so the compiler's placement holds those and the
// verifier draws no violation for them.
func TestVerify_AScalarOrUnionHoldingAnyBoundIsNotChecked(t *testing.T) {
	t.Parallel()
	count, lower := new(int64(1)), ir.BigVal("1")
	base := ir.TypeRef{Target: "t/x/Leaf"}
	doc := stamped(closedDoc(
		&ir.Scalar{ID: "t/x/S", Base: &base, Constraints: &ir.Constraints{MinLength: count, Min: &lower, MinProps: count}},
		&ir.Union{ID: "t/x/U", Variants: []ir.Variant{{Type: base}}, Constraints: &ir.Constraints{MinLength: count, Min: &lower}},
	))
	assert.Empty(t, boundViolations(doc))
}
