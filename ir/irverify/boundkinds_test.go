// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// boundViolations returns the violations of ir/bound-of-another-kind in doc.
func boundViolations(doc *ir.Document) []irverify.Violation {
	var found []irverify.Violation
	for _, violation := range irverify.Verify(doc) {
		if violation.Code == "ir/bound-of-another-kind" {
			found = append(found, violation)
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
	base := ir.TypeRef{Target: "type/x/Leaf"}
	doc := stamped(closedDoc(
		&ir.Scalar{ID: "type/x/S", Base: &base, Constraints: &ir.Constraints{MinLength: count, Min: &lower, MinProperties: count}},
		&ir.Union{ID: "type/x/U", Variants: []ir.Variant{{Type: base}}, Constraints: &ir.Constraints{MinLength: count, Min: &lower}},
	))
	assert.Empty(t, boundViolations(doc))
}

// TestVerify_ABoundOnANodeWhoseKindCannotTakeItIsAViolation holds that a model
// holding a string bound, a list holding a property count, a map holding a
// numeric bound and a tuple holding a pattern each draw one violation at the
// node, and that the bounds each of those kinds takes draw none.
func TestVerify_ABoundOnANodeWhoseKindCannotTakeItIsAViolation(t *testing.T) {
	t.Parallel()
	count, lower := new(int64(1)), ir.BigVal("1")
	stringType := ir.TypeRef{Target: "type/x/Leaf"}
	for _, tc := range []struct {
		name string
		td   ir.TypeDef
		bad  bool
	}{
		{"a model's string bound", &ir.Model{Constraints: &ir.Constraints{MinLength: count}}, true},
		{"a model's pattern", &ir.Model{Constraints: &ir.Constraints{Pattern: "a"}}, true},
		{"a model's item count", &ir.Model{Constraints: &ir.Constraints{MaxItems: count}}, true},
		{"a model's property counts", &ir.Model{Constraints: &ir.Constraints{MinProperties: count, MaxProperties: count}}, false},
		{"a list's property count", &ir.List{ElementType: stringType, Constraints: &ir.Constraints{MinProperties: count}}, true},
		{"a list's string bound", &ir.List{ElementType: stringType, Constraints: &ir.Constraints{MaxLength: count}}, true},
		{"a list's item bounds", &ir.List{ElementType: stringType, Constraints: &ir.Constraints{
			MinItems: count, MaxItems: count, UniqueItems: true,
		}}, false},
		{"a map's numeric bound", &ir.MapT{Value: stringType, Constraints: &ir.Constraints{Min: &lower}}, true},
		{"a map's item count", &ir.MapT{Value: stringType, Constraints: &ir.Constraints{MinItems: count}}, true},
		{"a map's property counts", &ir.MapT{Value: stringType, Constraints: &ir.Constraints{MinProperties: count, MaxProperties: count}}, false},
		{"a tuple's pattern", &ir.Tuple{ElementTypes: []ir.TypeRef{stringType}, Constraints: &ir.Constraints{Pattern: "a"}}, true},
		{"a tuple's item bounds", &ir.Tuple{ElementTypes: []ir.TypeRef{stringType}, Constraints: &ir.Constraints{
			MinItems: count, MaxItems: count, UniqueItems: true,
		}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			td := tc.td
			td.Common().ID = "type/x/N"
			got := boundViolations(stamped(closedDoc(td)))
			if !tc.bad {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Contains(t, got[0].Path, "type/x/N", "the violation is at the node")
		})
	}
}
