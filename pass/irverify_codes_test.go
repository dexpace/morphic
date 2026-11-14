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
	prim := &ir.Primitive{ID: "t/prim/string", Name: name("string"), Prim: ir.PrimString}
	prim.Provenance = ir.Provenance{Source: ir.NoSource}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces: map[string][]string{
			"t": {"x"}, "op": {"x"}, "s": {"x"}, "g": {"x"}, "p": {"x"}, "param": {"x"}, "e": {"x"},
		},
		Types: ir.TypeRegistry{
			prim.ID: prim,
			"t/x/M": &ir.Model{ID: "t/x/M", Name: name("m"), Properties: []ir.Property{
				{ID: "p/x/M/a", Name: name("a"), Type: ir.TypeRef{Target: "t/prim/string"}},
			}},
			"t/x/E": &ir.Enum{ID: "t/x/E", Name: name("e"), ValueType: ir.PrimString, Members: []ir.EnumMember{
				{ID: "e/x/E/s:a", Name: name("a"), Value: ir.Value{Kind: ir.ValueString, Str: "a"}},
			}},
		},
		Services: []ir.Service{{ID: "s/x/S", Name: name("s"), Groups: []ir.OperationGroup{{
			ID: "g/x/G", Name: name("g"),
			Operations: []ir.Operation{{
				ID: "op/x/o", Name: name("o"),
				Params: []ir.Parameter{{ID: "param/x/o/q", Name: name("q"), Type: ir.TypeRef{Target: "t/prim/string"}}},
				Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
					Method: "GET", URITemplate: "/o",
					ParamBindings: []ir.HTTPParamBinding{{Param: "param/x/o/q", Location: ir.HTTPLocationQuery}},
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
	for _, d := range diags {
		codes = append(codes, d.Code)
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
	model := func(d *ir.Document) *ir.Model { return d.Types["t/x/M"].(*ir.Model) }
	op := func(d *ir.Document) *ir.Operation { return &d.Services[0].Groups[0].Operations[0] }
	channel := ir.Naming{Source: "c", Canonical: "c"}
	for _, tc := range []struct {
		name string
		code string
		plan func(d *ir.Document)
	}{
		{"additional mode", "ir/unknown-additional-mode", func(d *ir.Document) { model(d).Additional = "bogus" }},
		{"presence", "ir/unknown-presence-kind", func(d *ir.Document) { model(d).Properties[0].Presence = "bogus" }},
		{"http location", "ir/unknown-http-location", func(d *ir.Document) {
			op(d).Bindings.HTTP[0].ParamBindings[0].Location = ""
		}},
		{"idempotency kind", "ir/unknown-idempotency-kind", func(d *ir.Document) { op(d).Idempotency.Kind = "bogus" }},
		{"page strategy", "ir/unknown-page-strategy", func(d *ir.Document) {
			op(d).Pagination = &ir.Pagination{Strategy: "bogus"}
		}},
		{"message direction", "ir/unknown-message-direction", func(d *ir.Document) {
			d.Channels = map[ir.ChannelID]ir.Channel{"chan/x/c": {ID: "chan/x/c", Name: channel}}
			d.IDSpaces[ir.IDKindChannel] = []string{"x"}
			op(d).Bindings.Message = &ir.MessageBinding{Channel: "chan/x/c", Direction: "bogus"}
		}},
		{"severity", "ir/unknown-severity", func(d *ir.Document) {
			d.Diagnostics = []ir.Diagnostic{{
				Severity: "bogus", Code: "x/y", Message: "m", Provenance: ir.Provenance{Source: ir.NoSource},
			}}
		}},
		{"blank version", "ir/version-label-blank", func(d *ir.Document) { d.Versions = []string{"v1", ""} }},
		{"repeated version", "ir/version-label-repeated", func(d *ir.Document) { d.Versions = []string{"v1", "v1"} }},
		{"undeclared version", "ir/version-label-undeclared", func(d *ir.Document) {
			d.Versions = []string{"v1"}
			model(d).Availability = &ir.Availability{Added: []string{"v9"}}
		}},
		{"availability order", "ir/availability-out-of-order", func(d *ir.Document) {
			d.Versions = []string{"v1", "v2"}
			model(d).Availability = &ir.Availability{Added: []string{"v2", "v1"}}
		}},
		{"primitive source", "ir/prim-names-source", func(d *ir.Document) {
			d.Sources = []ir.SourceInfo{{Format: "openapi@3.1", Path: "api.yaml"}}
			d.Types["t/prim/string"].Common().Provenance = ir.Provenance{Source: 0}
		}},
		{"enum member kind", "ir/enum-member-kind", func(d *ir.Document) {
			d.Types["t/x/E"].(*ir.Enum).ValueType = ir.PrimInteger
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			planted := verifyOnlyBase()
			tc.plan(planted)

			violations := irverify.Verify(planted)
			codes := make([]string, 0, len(violations))
			for _, v := range violations {
				codes = append(codes, v.Code)
			}
			require.Contains(t, codes, tc.code, "the plant is one irverify refuses")
			assert.Equal(t, validateCodes(t, verifyOnlyBase()), validateCodes(t, planted),
				"pass.Validate reports nothing of its own on a document irverify refuses under %s", tc.code)
		})
	}
}
