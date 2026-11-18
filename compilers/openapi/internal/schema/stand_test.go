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

// TestStand_AUnionKeptBesideAPrincipalsBodyStandsAtThePosition pins an inline
// principal writing a union whose branches declare no shape beside a body that
// lowers to a shared primitive: the alias keeping the union stands at the
// position, as it does where the same keywords are written beside each other,
// the union kept where it is written (docs/compilers/openapi.md#allof-refinement).
func TestStand_AUnionKeptBesideAPrincipalsBodyStandsAtThePosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    Beside: {description: d, type: string, oneOf: [{minLength: 1}, {maxLength: 3}]}\n"+
			"    Entry: {allOf: [{description: d}, {type: string, oneOf: [{minLength: 1}, {maxLength: 3}]}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	for name, at := range map[string]string{"Beside": "/components/schemas/Beside/oneOf", "Entry": "/components/schemas/Entry/allOf/1/oneOf"} {
		alias, ok := typeByName(doc, name).(*ir.Scalar)
		require.True(t, ok, "%s is the alias keeping the union, not %T", name, typeByName(doc, name))
		require.NotNil(t, alias.Base)
		assert.Equal(t, ir.TypeID("t/prim/string"), alias.Base.Target, name)
		assert.Equal(t, "d", alias.Docs.Description, name)
		kept, held := alias.Unmodeled["openapi:oneOf"]
		require.True(t, held, name)
		assert.Equal(t, ir.ReasonValidationOnly, kept.Reason, name)
		assert.Equal(t, at, string(kept.Provenance.Pointer), "%s: kept where it is written", name)
	}
	_, atEntry := doc.Types["t/anon/components/schemas/Entry/allOf/1"]
	assert.False(t, atEntry, "nothing stands at the entry")
}
