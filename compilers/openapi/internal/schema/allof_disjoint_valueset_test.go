// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAllOf_AValueSetBesideAUnionWarnsOnce pins a value set sharing no kind
// with the $ref its allOf writes, beside a union too: in either spelling of
// the set the schema draws one warning, that its conjuncts share no value,
// and no second one for the union beside them.
func TestAllOf_AValueSetBesideAUnionWarnsOnce(t *testing.T) {
	t.Parallel()
	const union = "oneOf: [{$ref: '#/components/schemas/Str'}, {type: boolean}]"
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    Obj: {type: object, properties: {a: {type: string}}}\n    Str: {type: string}\n"+
			"    Wrap: {allOf: [{$ref: '#/components/schemas/Obj'}], enum: [a, b], "+union+"}\n"+
			"    Entry: {allOf: [{$ref: '#/components/schemas/Obj'}, {enum: [a, b]}], "+union+"}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	var warnings []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Severity == ir.SeverityWarning {
			warnings = append(warnings, diagnostic)
		}
	}
	assert.Len(t, warnings, 2, "a warning for each spelling and no other: %v", warnings)
	for _, name := range []string{"Wrap", "Entry"} {
		at := diagsAtPointer(disjointReports(diags), "openapi/degraded-construct", "/components/schemas/"+name)
		assert.Len(t, at, 1, "%s: one warning that its conjuncts share no value", name)
		_, isEnum := typeByName(doc, name).(*ir.Enum)
		assert.True(t, isEnum, name)
	}
}
