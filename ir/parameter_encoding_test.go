// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestParameter_EncodingJSONContract pins Parameter.Encoding's encoding: it is
// the member "encoding", absent while the parameter has none, and a parameter
// holding one reads back as it was written.
func TestParameter_EncodingJSONContract(t *testing.T) {
	t.Parallel()
	bare, err := json.Marshal(ir.Parameter{ID: "q/a"})
	require.NoError(t, err)
	assert.NotContains(t, string(bare), `"encoding"`, "an absent encoding is no member")

	with := ir.Parameter{ID: "q/a", Encoding: &ir.Encoding{Name: "http-date"}}
	encoded, err := json.Marshal(with)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"encoding":{"name":"http-date"}`)

	full := ir.Parameter{ID: "q/a", Encoding: populatedEncoding()}
	encoded, err = json.Marshal(full)
	require.NoError(t, err)
	var back ir.Parameter
	require.NoError(t, json.Unmarshal(encoded, &back))
	assert.Equal(t, full.Encoding, back.Encoding, "every member of the encoding survives")
}

// TestTypeEdges_ReadsAParametersEncoding pins that a parameter's encoding
// names the types it names to the graph as a property's does: the type its
// value is written as and the type it decodes to are both edges of the type
// whose property declares the parameter as a field argument, beside the
// parameter's own type, and TypeEdges agrees with the reflection walk.
func TestTypeEdges_ReadsAParametersEncoding(t *testing.T) {
	t.Parallel()
	m := irtest.ModelOf("t/m", func(m *ir.Model) {
		m.Properties = []ir.Property{{ID: "p/f", Type: ir.TypeRef{Target: "t/f"}, Args: []ir.Parameter{{
			ID: "q/a", Type: ir.TypeRef{Target: "t/arg"},
			Encoding: &ir.Encoding{WireType: &ir.TypeRef{Target: "t/x"}, Schema: &ir.TypeRef{Target: "t/y"}},
		}}}}
	})

	edges := slices.Collect(ir.TypeEdges(m))
	for _, id := range []ir.TypeID{"t/f", "t/arg", "t/x", "t/y"} {
		assert.Contains(t, edges, id)
	}
	assert.Empty(t, edgeDiff(m))
}
