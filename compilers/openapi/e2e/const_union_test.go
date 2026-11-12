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
// (docs/compilers/openapi.md#string-const-union): for consts, one-member enums,
// a union of one value and a branch of several values only the union names, the
// one the same values written as an enum lower to, member for member, each
// holding what its branch says of its value. No branch type is left behind, and
// a union whose branch of several values another union names or says something
// of all of, or that names a component or keeps a constraint, stays.
func TestStringConstUnion_NormalizesToItsEnumSpelling(t *testing.T) {
	t.Parallel()
	doc, _ := parseCorpus(t, "string-const-union")
	got, diags := pass.ConstUnions{}.Run(t.Context(), doc)
	require.Empty(t, diags)
	spelled := compileComponentSpec(t, openapitest.ComponentSpec(`
    Color: {type: string, enum: [red, green]}
    Level: {type: string, enum: [low, high]}
    Direction: {type: string, enum: [up, down]}
    Tier: {type: string, enum: [free, pro, enterprise]}
`))

	said := map[string][]ir.EnumMember{
		"Color":     {{Docs: ir.Docs{Summary: "Red", Description: "The colour red."}}, {Deprecation: &ir.Deprecation{}}},
		"Level":     {{Docs: ir.Docs{Description: "Low."}}, {}},
		"Direction": {{}, {}},
		"Tier":      {{}, {}, {}},
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

	for _, kept := range []string{"Shade", "Pinned", "Shared", "Reuse", "Described"} {
		assert.Same(t, doc.Types[componentID(kept)], got.Types[componentID(kept)], "%s stays", kept)
	}
	for id := range got.Types {
		for _, name := range []string{"Color", "Level", "Direction", "Tier"} {
			assert.False(t, strings.HasPrefix(string(id), string(componentID(name))+"/"),
				"%s's branch type %s is left behind", name, id)
		}
	}
}

// TestStringConstUnion_TakesAChainOfDefsInOneRun pins a chain of unions each
// naming the next through $defs, as a document writes one: every link is taken
// in the one run, the first holding every value in order and none of the others
// left, and a second run finds nothing to do.
func TestStringConstUnion_TakesAChainOfDefsInOneRun(t *testing.T) {
	t.Parallel()
	doc := compileComponentSpec(t, openapitest.ComponentSpec(`
    Root:
      $defs:
        X0: {oneOf: [{$ref: '#/components/schemas/Root/$defs/X1'}, {const: a0}]}
        X1: {oneOf: [{$ref: '#/components/schemas/Root/$defs/X2'}, {const: a1}]}
        X2: {oneOf: [{const: a2}, {const: a3}]}
      oneOf:
        - $ref: '#/components/schemas/Root/$defs/X0'
        - {const: r}
`))
	got, diags := pass.ConstUnions{}.Run(t.Context(), doc)
	require.Empty(t, diags)

	root, ok := got.Types[componentID("Root")].(*ir.Enum)
	require.True(t, ok, "Root is an enum")
	values := make([]string, 0, len(root.Members))
	for _, m := range root.Members {
		values = append(values, m.Value.Str)
	}
	assert.Equal(t, []string{"a2", "a3", "a1", "a0", "r"}, values)
	assert.Len(t, got.Types, 1, "no link of the chain is left behind")

	again, diags := pass.ConstUnions{}.Run(t.Context(), got)
	require.Empty(t, diags)
	assert.Same(t, got, again)
}
