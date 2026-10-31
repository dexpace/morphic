// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestAllOf_ACollisionPastTheFlatteningDepthIsRefused pins the one way two folds
// reach one field pointer with different sets: a wrapper reads a declaration
// nested just past the depth a model enclosing it flattens to. The field pointer
// cannot tell them apart, so the registry refuses it rather than let one replace
// the other, and one level shallower both read it and share the node.
func TestAllOf_ACollisionPastTheFlatteningDepthIsRefused(t *testing.T) {
	t.Parallel()
	spec := func(depth int) string {
		branch := "{type: object, properties: {data: {$ref: '#/components/schemas/C'}}}"
		for range depth {
			branch = "{allOf: [" + branch + "]}"
		}
		return openapitest.ComponentSpec(fmt.Sprintf(`    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    C: {type: object, properties: {c: {type: string}}}
    P: {type: object, properties: {p: {$ref: '#/components/schemas/Outer/allOf/0'}}}
    Outer:
      allOf:
        - allOf:
            - {type: object, properties: {data: {$ref: '#/components/schemas/A'}}}
            - {type: object, properties: {data: {$ref: '#/components/schemas/B'}}}
            - %s
`, branch))
	}
	refused := func(diags []ir.Diagnostic) int {
		n := 0
		for _, d := range diags {
			if d.Code == compile.RegistryRefused {
				n++
			}
		}
		return n
	}
	for _, tc := range []struct {
		name  string
		depth int
		want  int
	}{
		{"within the depth of both", schema.MaxAllOfFlattenDepth - 1, 0},
		{"past the model's depth, within the wrapper's", schema.MaxAllOfFlattenDepth, 1},
	} {
		_, diags := parseFull(t, spec(tc.depth))
		assert.Equal(t, tc.want, refused(diags), tc.name)
	}
}
