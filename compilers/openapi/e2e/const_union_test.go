// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it holds the
// compiler and pass.ConstUnions together to one lowering rule, so it has no
// single source file to pair with.
package e2e_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestStringConstUnion_NormalizesToItsEnumSpelling holds the union of string
// values the compiler keeps to the Enum pass.ConstUnions makes of it
// (docs/compilers/openapi.md#string-const-union): for consts, one-member enums
// and both together, the one the same values written as an enum lower to,
// member for member, each holding what its branch says of its value. No branch
// type is left behind, and a union whose branch names a component or keeps a
// constraint stays.
func TestStringConstUnion_NormalizesToItsEnumSpelling(t *testing.T) {
	t.Parallel()
	doc, _ := parseCorpus(t, "string-const-union")
	got, diags := pass.ConstUnions{}.Run(t.Context(), doc)
	require.Empty(t, diags)
	spelled := compileComponentSpec(t, openapitest.ComponentSpec(`
    Color: {type: string, enum: [red, green]}
    Level: {type: string, enum: [low, high]}
    Tier: {type: string, enum: [free, pro, enterprise]}
`))

	said := map[string][]ir.EnumMember{
		"Color": {{Docs: ir.Docs{Summary: "Red", Description: "The colour red."}}, {Deprecation: &ir.Deprecation{}}},
		"Level": {{Docs: ir.Docs{Description: "Low."}}, {}},
		"Tier":  {{}, {}, {}},
	}
	for name, facts := range said {
		enum, ok := got.Types[componentID(name)].(*ir.Enum)
		require.True(t, ok, "%s is an enum", name)
		want, ok := spelled.Types[componentID(name)].(*ir.Enum)
		require.True(t, ok)
		assert.Equal(t, want.ValueType, enum.ValueType, name)
		assert.Equal(t, want.Closed, enum.Closed, name)
		require.Len(t, enum.Members, len(facts), name)
		for i, m := range want.Members {
			m.Docs, m.Deprecation = facts[i].Docs, facts[i].Deprecation
			assert.Empty(t, cmp.Diff(m, enum.Members[i]), "%s member %d", name, i)
		}
		assert.Equal(t, doc.Types[componentID(name)].Common().Provenance, enum.Provenance,
			"%s keeps the union's provenance", name)
	}

	assert.Same(t, doc.Types[componentID("Shade")], got.Types[componentID("Shade")])
	assert.Same(t, doc.Types[componentID("Pinned")], got.Types[componentID("Pinned")])
	for id := range got.Types {
		for _, name := range []string{"Color", "Level", "Tier"} {
			assert.False(t, strings.HasPrefix(string(id), string(componentID(name))+"/"),
				"%s's branch type %s is left behind", name, id)
		}
	}
}
