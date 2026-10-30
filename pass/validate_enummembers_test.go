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
	doc.Types["t/e"] = &ir.Enum{ID: "t/e", ValueType: ir.PrimString, Closed: true, Members: []ir.EnumMember{
		{ID: "e/x/e/s:a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
		{ID: "e/x/e/s:b", Value: ir.Value{Kind: ir.ValueString, Str: "b"}},
		{ID: "e/x/e/s:a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
		{Value: ir.Value{Kind: ir.ValueString, Str: "c"}},
		{Value: ir.Value{Kind: ir.ValueString, Str: "d"}},
	}}
	doc.Types["t/f"] = &ir.Enum{ID: "t/f", ValueType: ir.PrimString, Closed: true, Members: []ir.EnumMember{
		{ID: "e/x/e/s:a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
	}}

	var at []string
	for _, d := range validated(t, doc) {
		if d.Code == "pass/duplicate-enum-member-id" {
			assert.Equal(t, ir.SeverityError, d.Severity)
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"t/e/members/2"}, at)
}

func TestValidate_DuplicateEnumValuesAreLegal(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/e"] = &ir.Enum{
		ID:        "t/e",
		ValueType: ir.PrimString,
		Members: []ir.EnumMember{
			{Name: ir.Naming{Source: "a"}, Value: ir.Value{Kind: ir.ValueString, Str: "x"}},
			{Name: ir.Naming{Source: "b"}, Value: ir.Value{Kind: ir.ValueString, Str: "x"}},
		},
	}
	assert.Empty(t, validated(t, doc))
}
