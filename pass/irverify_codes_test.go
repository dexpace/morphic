// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
	"github.com/dexpace/morphic/pass"
)

// verifyOnlyBase is a small document with a model, an enum and one operation
// bound once, which the cases below plant a value irverify refuses on.
func verifyOnlyBase() *ir.Document {
	name := func(s string) ir.Naming { return ir.Naming{Source: s, Canonical: s} }
	primitive := &ir.Primitive{ID: "type/primitive/string", Name: name("string"), PrimitiveKind: ir.PrimitiveString}
	primitive.Provenance = ir.Provenance{Source: ir.NoSource}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces: map[string][]string{
			"type": {"x"}, "operation": {"x"}, "service": {"x"}, "operation-group": {"x"}, "property": {"x"}, "parameter": {"x"}, "enum-member": {"x"},
		},
		Types: ir.TypeRegistry{
			primitive.ID: primitive,
			"type/x/M": &ir.Model{ID: "type/x/M", Name: name("m"), Properties: []ir.Property{
				{ID: "property/x/M/a", Name: name("a"), Type: ir.TypeRef{Target: "type/primitive/string"}},
			}},
			"type/x/E": &ir.Enum{ID: "type/x/E", Name: name("e"), ValueType: ir.PrimitiveString, Members: []ir.EnumMember{
				{ID: "enum-member/x/E/string:a", Name: name("a"), Value: ir.Value{Kind: ir.ValueString, String: "a"}},
			}},
		},
		Services: []ir.Service{{ID: "service/x/S", Name: name("s"), Groups: []ir.OperationGroup{{
			ID: "operation-group/x/G", Name: name("g"),
			Operations: []ir.Operation{{
				ID: "operation/x/o", Name: name("o"),
				Params: []ir.Parameter{{ID: "parameter/x/o/q", Name: name("q"), Type: ir.TypeRef{Target: "type/primitive/string"}}},
				Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{
					Method: "GET", URITemplate: "/o",
					ParamBindings: []ir.HTTPParamBinding{{Param: "parameter/x/o/q", Location: ir.HTTPLocationQuery}},
				}}},
			}},
		}}}},
	}
}

// validateCodes returns the sorted codes pass.Validate reports on doc.
func validateCodes(t *testing.T, doc *ir.Document) []string {
	t.Helper()
	diags, err := pass.Validate(t.Context(), doc)
	require.NoError(t, err)
	codes := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		codes = append(codes, diagnostic.Code)
	}
	slices.Sort(codes)
	return codes
}

// TestValidate_StaysSilentOnWhatIrverifyHolds drives each document irverify
// refuses under a code of its own through pass.Validate beside the nearest valid
// document: the two draw the same diagnostics, so each defect has one reporter,
// and the code is irverify's.
func TestValidate_StaysSilentOnWhatIrverifyHolds(t *testing.T) {
	t.Parallel()
	model := func(document *ir.Document) *ir.Model { return document.Types["type/x/M"].(*ir.Model) }
	operation := func(document *ir.Document) *ir.Operation { return &document.Services[0].Groups[0].Operations[0] }
	channel := ir.Naming{Source: "c", Canonical: "c"}
	for _, tc := range []struct {
		name string
		code string
		plan func(document *ir.Document)
	}{
		{"additional mode", "ir/unknown-additional-mode", func(document *ir.Document) { model(document).Additional = "bogus" }},
		{"presence", "ir/unknown-presence-kind", func(document *ir.Document) { model(document).Properties[0].Presence = "bogus" }},
		{"http location", "ir/unknown-http-location", func(document *ir.Document) {
			operation(document).Bindings.HTTP[0].ParamBindings[0].Location = ""
		}},
		{"idempotency kind", "ir/unknown-idempotency-kind", func(document *ir.Document) { operation(document).Idempotency.Kind = "bogus" }},
		{"page strategy", "ir/unknown-page-strategy", func(document *ir.Document) {
			operation(document).Pagination = &ir.Pagination{Strategy: "bogus"}
		}},
		{"message direction", "ir/unknown-message-direction", func(document *ir.Document) {
			document.Channels = map[ir.ChannelID]ir.Channel{"channel/x/c": {ID: "channel/x/c", Name: channel}}
			document.IDSpaces[ir.IDKindChannel] = []string{"x"}
			operation(document).Bindings.Message = &ir.MessageBinding{Channel: "channel/x/c", Direction: "bogus"}
		}},
		{"severity", "ir/unknown-severity", func(document *ir.Document) {
			document.Diagnostics = []ir.Diagnostic{{
				Severity: "bogus", Code: "x/y", Message: "m", Provenance: ir.Provenance{Source: ir.NoSource},
			}}
		}},
		{"blank version", "ir/version-label-blank", func(document *ir.Document) { document.Versions = []string{"v1", ""} }},
		{"repeated version", "ir/version-label-repeated", func(document *ir.Document) { document.Versions = []string{"v1", "v1"} }},
		{"undeclared version", "ir/version-label-undeclared", func(document *ir.Document) {
			document.Versions = []string{"v1"}
			model(document).Availability = &ir.Availability{Added: []string{"v9"}}
		}},
		{"availability order", "ir/availability-out-of-order", func(document *ir.Document) {
			document.Versions = []string{"v1", "v2"}
			model(document).Availability = &ir.Availability{Added: []string{"v2", "v1"}}
		}},
		{"primitive source", "ir/prim-names-source", func(document *ir.Document) {
			document.Sources = []ir.SourceInfo{{Format: "openapi@3.1", Path: "api.yaml"}}
			document.Types["type/primitive/string"].Common().Provenance = ir.Provenance{Source: 0}
		}},
		{"enum member kind", "ir/enum-member-kind", func(document *ir.Document) {
			document.Types["type/x/E"].(*ir.Enum).ValueType = ir.PrimitiveInteger
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			planted := verifyOnlyBase()
			tc.plan(planted)

			violations := irverify.Verify(planted)
			codes := make([]string, 0, len(violations))
			for _, violation := range violations {
				codes = append(codes, violation.Code)
			}
			require.Contains(t, codes, tc.code, "the plant is one irverify refuses")
			assert.Equal(t, validateCodes(t, verifyOnlyBase()), validateCodes(t, planted),
				"pass.Validate reports nothing of its own on a document irverify refuses under %s", tc.code)
		})
	}
}
