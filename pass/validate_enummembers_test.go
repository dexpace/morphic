// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_DuplicateEnumMemberIDIsAnError pins that two members of one enum
// may not share an ID, and that the same ID in two enums is no repeat: an
// emitter keys collision resolution by it within an enum.
func TestValidate_DuplicateEnumMemberIDIsAnError(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/e"] = &ir.Enum{ID: "type/e", ValueType: ir.PrimitiveString, Closed: true, Members: []ir.EnumMember{
		{ID: "enum-member/x/e/string:a", Value: ir.Value{Kind: ir.ValueString, String: "a"}},
		{ID: "enum-member/x/e/string:b", Value: ir.Value{Kind: ir.ValueString, String: "b"}},
		{ID: "enum-member/x/e/string:a", Value: ir.Value{Kind: ir.ValueString, String: "a"}},
		{Value: ir.Value{Kind: ir.ValueString, String: "c"}},
		{Value: ir.Value{Kind: ir.ValueString, String: "d"}},
	}}
	doc.Types["type/f"] = &ir.Enum{ID: "type/f", ValueType: ir.PrimitiveString, Closed: true, Members: []ir.EnumMember{
		{ID: "enum-member/x/e/string:a", Value: ir.Value{Kind: ir.ValueString, String: "a"}},
	}}

	var at []string
	for _, diagnostic := range validated(t, doc) {
		if diagnostic.Code == "pass/duplicate-enum-member-id" {
			assert.Equal(t, ir.SeverityError, diagnostic.Severity)
			at = append(at, diagnostic.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"type/e/members/2"}, at)
}

func TestValidate_DuplicateEnumValuesAreLegal(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/e"] = &ir.Enum{
		ID:        "type/e",
		ValueType: ir.PrimitiveString,
		Members: []ir.EnumMember{
			{Name: ir.Naming{Source: "a"}, Value: ir.Value{Kind: ir.ValueString, String: "x"}},
			{Name: ir.Naming{Source: "b"}, Value: ir.Value{Kind: ir.ValueString, String: "x"}},
		},
	}
	assert.Empty(t, validated(t, doc))
}
