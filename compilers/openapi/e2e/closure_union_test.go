// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// closureUnion is a oneOf of two inline branches, each declaring the member it
// requires.
const closureUnion = "oneOf: [{properties: {x: {type: string}}, required: [x]}, " +
	"{properties: {y: {type: string}}, required: [y]}]"

// TestEntryClosure_AClosureBesideAUnionsOwnMembersIsKept holds that an entry's
// additionalProperties does not close the variants a union distributes into
// where the union's inline branches declare members: the keyword reads its own
// object's properties alone, so it applies to those members too, as it does to
// an entry's, and the same conjunction written out keeps the entry. Each
// variant stays open and keeps the closing entry, beside the allOf's union and
// on an entry's, in both orders of the entries.
func TestEntryClosure_AClosureBesideAUnionsOwnMembersIsKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, key string
	}{
		{"beside", "{allOf: [" + closureRef + ", {additionalProperties: false}], " + closureUnion + "}", "openapi:allOf/1"},
		{"beside-reversed", "{allOf: [{additionalProperties: false}, " + closureRef + "], " + closureUnion + "}", "openapi:allOf/0"},
		{"another-entry", "{allOf: [{" + closureUnion + "}, {additionalProperties: false}]}", "openapi:allOf/1"},
		{"another-entry-reversed", "{allOf: [{additionalProperties: false}, {" + closureUnion + "}]}", "openapi:allOf/0"},
		{"the-closing-entry", "{allOf: [{type: object, additionalProperties: false, " + closureUnion + "}, " + closureRef + "]}", "openapi:allOf/0"},
	} {
		for _, version := range []string{"3.0.3", "3.1.0"} {
			t.Run(fmt.Sprintf("%s/%s", tc.name, version), func(t *testing.T) {
				t.Parallel()
				doc, diags := compileAnnotationSpec(t, "union", conjunctionSpec(version, tc.schema, closureUser))
				requireErrorsAndWarnings(t, diags, nil)
				for _, at := range conjunctionPositions {
					u, ok := kindNode(t, doc, usedAt(t, doc, at)).(*ir.Union)
					require.True(t, ok, "a union at %s", at)
					require.Len(t, u.Variants, 2, at)
					for _, v := range u.Variants {
						variant, ok := kindNode(t, doc, v.Type.Target).(*ir.Model)
						require.True(t, ok, "a composed variant at %s", at)
						assert.Equal(t, ir.AdditionalUnspecified, variant.Additional, "open at %s", at)
						assert.Equal(t, ir.ReasonDegradedLowering, keptOn(variant)[tc.key], "the closing entry kept at %s", at)
					}
				}
			})
		}
	}
}
