// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// registryCase is a family that ranges the type registry: the type that draws
// its finding, its code, and where the finding sits at a type of that ID.
type registryCase struct {
	name, code string
	typeOf     func(id ir.TypeID) ir.TypeDef
	at         func(id ir.TypeID) string
}

// registryCases are the families that range the type registry through
// liveTypeIDs, each with the type that draws one finding.
func registryCases() []registryCase {
	stringType := ir.TypeRef{Target: "type/primitive/string"}
	return []registryCase{
		{
			name: "checkDiscriminators", code: "pass/discriminator-missing-variant",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Union{
					ID:            id,
					Variants:      []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}},
					Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{"a": "type/none"}},
				}
			},
			at: func(id ir.TypeID) string { return string(id) },
		},
		{
			name: "checkRepeatedVariants", code: "pass/inclusive-union-redundant-variant",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Union{ID: id, Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}, {Type: ir.TypeRef{Target: "type/m"}}}}
			},
			at: func(id ir.TypeID) string { return string(id) + "/variants/1" },
		},
		{
			name: "checkDuplicateMemberIDs", code: "pass/duplicate-enum-member-id",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Enum{ID: id, ValueType: ir.PrimitiveString, Closed: true, Members: []ir.EnumMember{
					{ID: "enum-member/a", Value: ir.Value{Kind: ir.ValueString, String: "a"}},
					{ID: "enum-member/a", Value: ir.Value{Kind: ir.ValueString, String: "a"}},
				}}
			},
			at: func(id ir.TypeID) string { return string(id) + "/members/1" },
		},
		{
			name: "checkDuplicateWireNames", code: "pass/duplicate-wire-name",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Model{ID: id, Properties: []ir.Property{
					{ID: ir.PropertyID("property/" + id + "/a"), WireName: "a", Type: stringType},
					{ID: ir.PropertyID("property/" + id + "/b"), WireName: "a", Type: stringType},
				}}
			},
			at: func(id ir.TypeID) string { return string(id) },
		},
		{
			name: "checkAmbiguousAliases", code: "pass/ambiguous-alias",
			typeOf: func(id ir.TypeID) ir.TypeDef {
				return &ir.Model{ID: id, Properties: []ir.Property{
					{ID: ir.PropertyID("property/" + id + "/a"), WireName: "a", Type: stringType, Name: ir.Naming{Source: "a", Canonical: "a", Aliases: []string{"b"}}},
					{ID: ir.PropertyID("property/" + id + "/b"), WireName: "b", Type: stringType, Name: ir.Naming{Source: "b", Canonical: "b"}},
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
}

// TestValidate_TypeRegistryDiagnosticsAreDeterministic pins invariant 7 at each
// check that ranges the type registry through liveTypeIDs. Go ranges the
// registry's keys in randomized order: each check reads the sorted IDs, so one
// that ranged the registry itself would redden its row.
func TestValidate_TypeRegistryDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	for _, tc := range registryCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			for _, id := range []ir.TypeID{"type/z", "type/b", "type/q", "type/c", "type/m2"} {
				doc.Types[id] = tc.typeOf(id)
			}
			want := pointers(withCode(validated(t, doc), tc.code))
			require.Equal(t, []string{tc.at("type/b"), tc.at("type/c"), tc.at("type/m2"), tc.at("type/q"), tc.at("type/z")}, want)
		})
	}
}
