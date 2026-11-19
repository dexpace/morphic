// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestTagDef_HoldsItsKeptKeys holds a tag's Unmodeled to its JSON name, omitted
// when empty, and a group's Inferred likewise.
func TestTagDef_HoldsItsKeptKeys(t *testing.T) {
	t.Parallel()
	tag := ir.TagDef{Name: "T", Unmodeled: ir.Unmodeled{
		"openapi:x-note": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`"n"`)},
	}}
	got, err := json.Marshal(tag, json.Deterministic(true))
	require.NoError(t, err)
	assert.Contains(t, string(got), `"unmodeled":{"openapi:x-note":`)
	var back ir.TagDef
	require.NoError(t, json.Unmarshal(got, &back))
	assert.Empty(t, cmp.Diff(tag, back))

	bare, err := json.Marshal(ir.TagDef{Name: "T"}, json.Deterministic(true))
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "unmodeled")

	group, err := json.Marshal(ir.OperationGroup{ID: "operation-group/x", Inferred: "extension-promotion"}, json.Deterministic(true))
	require.NoError(t, err)
	assert.Contains(t, string(group), `"inferred":"extension-promotion"`)
	plain, err := json.Marshal(ir.OperationGroup{ID: "operation-group/x"}, json.Deterministic(true))
	require.NoError(t, err)
	assert.NotContains(t, string(plain), "inferred")
}
