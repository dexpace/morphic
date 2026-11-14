// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

// TestUnder_KeysEachEntryBeneathTheScope holds Under to inserting the scope
// after the format's prefix of every key, the entry unchanged, src unwritten,
// and an empty src to nil.
func TestUnder_KeysEachEntryBeneathTheScope(t *testing.T) {
	t.Parallel()
	entry := ir.UnmodeledEntry{
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue(`1`),
		Provenance: ir.Provenance{Pointer: "/p/schema/x-a"},
	}
	src := ir.Unmodeled{"openapi:x-a": entry, "openapi:allOf/1": entry}
	assert.Empty(t, cmp.Diff(ir.Unmodeled{"openapi:schema/x-a": entry, "openapi:schema/allOf/1": entry},
		annotation.Under("schema", src)))
	assert.Len(t, src, 2, "src is not written")
	assert.Nil(t, annotation.Under("schema", nil))
}
