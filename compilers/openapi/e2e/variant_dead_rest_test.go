// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestCompile_ADeadBranchIsReadAgainstWhatTheRestAdmits holds the
// dead-branch warning to the rest of the allOf it names: none where the rest
// is a reference naming no schema, and where the rest shares no kind with the
// union, the one warning that its conjuncts share no value.
func TestCompile_ADeadBranchIsReadAgainstWhatTheRestAdmits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		want         []wantFinding
	}{
		{"a string rest beside an object branch",
			"P: {allOf: [{$ref: '#/components/schemas/Str'}, {oneOf: [{type: boolean}, {$ref: '#/components/schemas/Obj'}]}]}",
			[]wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/P", contains: []string{"share no value"}}}},
		{"a rest naming no schema",
			"P: {allOf: [{$ref: '#/components/schemas/Nowhere'}, {oneOf: [{type: string}, {type: integer}]}]}",
			[]wantFinding{{code: diag.UnresolvedRef, pointer: "/components/schemas/P/allOf/0"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := admissionSpec("3.1.0", "", "Obj: {type: object, properties: {a: {type: string}}}",
				"Str: {type: string}", tc.schema)
			_, diags := compileAnnotationSpec(t, "rest", spec)
			requireErrorsAndWarnings(t, diags, tc.want)
		})
	}
}

// TestCompile_ARestOfAValueSetIsNotSaidToRefuseABranchItAdmits holds that a
// union on an entry beside its own enum, next to an enum whose members a
// branch admits, draws no warning saying that no branch admits a value of the
// rest: the string branch admits "shared".
func TestCompile_ARestOfAValueSetIsNotSaidToRefuseABranchItAdmits(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "",
		"P: {allOf: [{enum: [a], oneOf: [{type: string}, {type: integer}]}, {enum: [b, shared]}]}")
	_, diags := compileAnnotationSpec(t, "valueset", spec)
	for _, d := range diags {
		assert.NotContains(t, d.Message, "admits a value the rest of the allOf admits", "%s", d.Message)
	}
}
