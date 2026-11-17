// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"testing"
)

// TestEntryValueSet_ARefWithTheValueSetBesideItIsTheSameNode holds that at
// 3.1, where a keyword beside a $ref applies, the value set written on the
// $ref's own object stands at the position and keeps the $ref as the
// spellings with an allOf do.
//
// It waits on a $ref and the keywords beside it lowering as one conjunction,
// as the allOf spellings do (docs/compilers/openapi.md#ref-use-site): until
// then the $ref site is a use of its target and keeps the value set beside it.
func TestEntryValueSet_ARefWithTheValueSetBesideItIsTheSameNode(t *testing.T) {
	t.Parallel()
	for _, vs := range entryValueSets() {
		t.Run(vs.name, func(t *testing.T) {
			t.Parallel()
			schema := "{$ref: '#/components/schemas/T', " + vs.keyword + "}"
			inBothMappingOrders(t, conjunctionSpec("3.1.0", schema, vs.target), func(t *testing.T, spec string) {
				doc, diags := compileAnnotationSpec(t, "beside", spec)
				requireErrorsAndWarnings(t, diags, nil)
				requireNoEntryIndexedType(t, doc)
				for _, at := range conjunctionPositions {
					requireEntryNodeAt(t, doc, diags, at, vs, -1)
				}
			})
		})
	}
}
