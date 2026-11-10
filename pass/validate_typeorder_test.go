// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_TypeRegistryDiagnosticsAreDeterministic pins invariant 7 at each
// check that ranges the type registry through liveTypeIDs. Go ranges the
// registry's keys in randomized order: each check reads the sorted IDs, so one
// that ranged the registry itself would redden its row.
func TestValidate_TypeRegistryDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	str := ir.TypeRef{Target: "t/prim/string"}
	cases := []struct {
		name, code string
		typeOf     func(id ir.TypeID) ir.TypeDef
		at         func(id ir.TypeID) string
	}{
		{
			name: "checkDiscriminators", code: "pass/discriminator-missing-variant",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Union{
					ID:            id,
					Variants:      []ir.Variant{{Type: ir.TypeRef{Target: "t/m"}}},
					Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{"a": "t/none"}},
				}
			},
			at: func(id ir.TypeID) string { return string(id) },
		},
		{
			name: "checkDuplicateMemberIDs", code: "pass/duplicate-enum-member-id",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Enum{ID: id, ValueType: ir.PrimString, Closed: true, Members: []ir.EnumMember{
					{ID: "e/a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
					{ID: "e/a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
				}}
			},
			at: func(id ir.TypeID) string { return string(id) + "/members/1" },
		},
		{
			name: "checkDuplicateWireNames", code: "pass/duplicate-wire-name",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Model{ID: id, Properties: []ir.Property{
					{ID: ir.PropID("p/" + id + "/a"), WireName: "a", Type: str},
					{ID: ir.PropID("p/" + id + "/b"), WireName: "a", Type: str},
				}}
			},
			at: func(id ir.TypeID) string { return string(id) },
		},
		{
			name: "checkAmbiguousAliases", code: "pass/ambiguous-alias",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Model{ID: id, Properties: []ir.Property{
					{ID: ir.PropID("p/" + id + "/a"), WireName: "a", Type: str, Name: ir.Naming{Source: "a", Canonical: "a", Aliases: []string{"b"}}},
					{ID: ir.PropID("p/" + id + "/b"), WireName: "b", Type: str, Name: ir.Naming{Source: "b", Canonical: "b"}},
				}}
			},
			at: func(id ir.TypeID) string { return string(id) + "/properties/0" },
		},
		{
			name: "checkArgsOutsideGraphQL", code: "pass/args-outside-graphql",
			typeOf: func(id ir.TypeID) ir.TypeDef { return argModel(id) },
			at:     func(id ir.TypeID) string { return string(id) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			for _, id := range []ir.TypeID{"t/z", "t/b", "t/q", "t/c", "t/m2"} {
				doc.Types[id] = tc.typeOf(id)
			}
			want := pointers(withCode(validated(t, doc), tc.code))
			require.Equal(t, []string{tc.at("t/b"), tc.at("t/c"), tc.at("t/m2"), tc.at("t/q"), tc.at("t/z")}, want)
			for range 8 {
				assert.Equal(t, want, pointers(withCode(validated(t, doc), tc.code)))
			}
		})
	}
}
