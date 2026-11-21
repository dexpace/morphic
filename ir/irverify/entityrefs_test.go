// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// entityRefFindings returns each finding checkEntityRefs makes of doc, its
// code then its path.
func entityRefFindings(doc *ir.Document) []string {
	var got []string
	for _, violation := range irverify.Verify(doc) {
		switch violation.Code {
		case "ir/entity-ref-empty", "ir/entity-ref-ambiguous", "ir/replaced-by-itself":
			got = append(got, violation.Code+" "+violation.Path)
		}
	}
	return got
}

// TestVerify_AnEntityRefNamesOneEntity holds a replacement to setting one field:
// none names nothing, two are ambiguous, one of any class is sound.
func TestVerify_AnEntityRefNamesOneEntity(t *testing.T) {
	t.Parallel()
	const at = "document.Services[0].Groups[0].Operations[0].Deprecation.ReplacedBy"
	for _, tc := range []struct {
		name string
		refs []ir.EntityRef
		want []string
	}{
		{"one of each kind", []ir.EntityRef{{Operation: "operation/x/S/v2"}, {Type: "type/x/V2"}, {Server: "server/x"}}, nil},
		{"none", []ir.EntityRef{{}}, []string{"ir/entity-ref-empty " + at + "[0]"}},
		{"two", []ir.EntityRef{{Operation: "operation/x/S/v2"}, {Operation: "operation/x/S/v3", Param: "parameter/x"}},
			[]string{"ir/entity-ref-ambiguous " + at + "[1]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			operation := ir.Operation{ID: "operation/x/S/op", Deprecation: &ir.Deprecation{ReplacedBy: tc.refs}}
			assert.Equal(t, tc.want, entityRefFindings(singleOperationDocument(operation)))
		})
	}
}

// TestVerify_AReplacementIsAReference pins that the reference check reads a
// replacement as it reads any typed ID: one naming nothing the document
// declares is dangling, at its field.
func TestVerify_AReplacementIsAReference(t *testing.T) {
	t.Parallel()
	operation := ir.Operation{ID: "operation/x/S/op", Deprecation: &ir.Deprecation{ReplacedBy: []ir.EntityRef{{Operation: "operation/x/S/ghost"}}}}
	assert.Equal(t, []string{"document.Services[0].Groups[0].Operations[0].Deprecation.ReplacedBy[0].Operation"},
		violationPaths(irverify.Verify(singleOperationDocument(operation)), "ir/dangling-operation-ref"))
}

// TestVerify_NothingReplacesItself holds every carrier of a Deprecation to
// naming another entity as its replacement: an operation, a parameter, a
// property, a type and an enum member each naming itself are reported, and a
// replacement of another ID, or of the same ID in another class, is not.
func TestVerify_NothingReplacesItself(t *testing.T) {
	t.Parallel()
	self := func(ref ir.EntityRef) *ir.Deprecation { return &ir.Deprecation{ReplacedBy: []ir.EntityRef{ref}} }
	operation := ir.Operation{
		ID: "operation/x/S/op", Deprecation: self(ir.EntityRef{Operation: "operation/x/S/op"}),
		Params: []ir.Parameter{
			{ID: "parameter/x/a", Name: named("a"), Deprecation: self(ir.EntityRef{Param: "parameter/x/a"})},
			{ID: "parameter/x/b", Name: named("b"), Deprecation: self(ir.EntityRef{Param: "parameter/x/a"})},
			{ID: "parameter/x/c", Name: named("c"), Deprecation: self(ir.EntityRef{Property: "parameter/x/c"})},
		},
	}
	doc := singleOperationDocument(operation)
	doc.Types = ir.TypeRegistry{
		"type/x/M": &ir.Model{ID: "type/x/M", Name: named("m"), Deprecation: self(ir.EntityRef{Type: "type/x/M"}),
			Properties: []ir.Property{{ID: "property/x/M/a", Name: named("a"), Deprecation: self(ir.EntityRef{Property: "property/x/M/a"})}}},
		"type/x/E": &ir.Enum{ID: "type/x/E", Name: named("e"), Members: []ir.EnumMember{
			{ID: "enum-member/x/E/a", Name: named("a"), Deprecation: self(ir.EntityRef{Member: "enum-member/x/E/a"})},
		}},
	}
	assert.ElementsMatch(t, []string{
		"ir/replaced-by-itself document.Services[0].Groups[0].Operations[0].Deprecation.ReplacedBy[0]",
		"ir/replaced-by-itself document.Services[0].Groups[0].Operations[0].Params[0].Deprecation.ReplacedBy[0]",
		"ir/replaced-by-itself document.Types[type/x/M].Deprecation.ReplacedBy[0]",
		"ir/replaced-by-itself document.Types[type/x/M].Properties[0].Deprecation.ReplacedBy[0]",
		"ir/replaced-by-itself document.Types[type/x/E].Members[0].Deprecation.ReplacedBy[0]",
	}, entityRefFindings(doc))
}
