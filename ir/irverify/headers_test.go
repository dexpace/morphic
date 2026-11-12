// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// prefixHeaderCodes are the codes checkPrefixHeaders reports.
var prefixHeaderCodes = []string{"ir/prefix-header-not-map", "ir/prefix-header-wire-name"}

// prefixHeaderTypes are the types the headers of TestVerify_AHeaderUnderAPrefixIsAMapOfNoName
// name: a map, an anonymous model decorating it, a model, and two anonymous
// models decorating each other.
func prefixHeaderTypes() []ir.TypeDef {
	str := ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString)}
	decorates := func(id, base ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Anonymous: true, Base: &ir.TypeRef{Target: base}}
	}
	return []ir.TypeDef{
		&ir.MapT{ID: "t/x/Meta", Name: named("meta"), Value: str},
		decorates("t/x/Meta/decorated", "t/x/Meta"),
		&ir.Model{ID: "t/x/Name", Name: named("name")},
		decorates("t/x/loop/a", "t/x/loop/b"),
		decorates("t/x/loop/b", "t/x/loop/a"),
	}
}

// prefixHeaderViolations returns the paths checkPrefixHeaders reports for a
// response declaring h, each prefixed by its code.
func prefixHeaderViolations(h ir.Header) []string {
	doc := opDoc(ir.Operation{ID: "op/x/S/op", Responses: []ir.Response{{Headers: []ir.Header{h}}}})
	doc.Types = ir.TypeRegistry{}
	for _, td := range prefixHeaderTypes() {
		doc.Types[td.Common().ID] = td
	}
	var got []string
	for _, v := range irverify.Verify(doc) {
		for _, code := range prefixHeaderCodes {
			if v.Code == code {
				got = append(got, code+" "+v.Path)
			}
		}
	}
	return got
}

// TestVerify_AHeaderUnderAPrefixIsAMapOfNoName holds a header under a prefix,
// the empty one included, to a type reaching a map through any decorators and
// to no wire name, and leaves a header of one field and a dangling type to the
// checks that own them (docs/ir/responses.md#prefix-headers).
func TestVerify_AHeaderUnderAPrefixIsAMapOfNoName(t *testing.T) {
	t.Parallel()
	const at = "doc.Services[0].Groups[0].Operations[0].Responses[0].Headers[0]"
	header := func(target ir.TypeID, wire string, prefix *string) ir.Header {
		return ir.Header{ID: "p/x/h", Name: named("meta"), WireName: wire, Type: ir.TypeRef{Target: target}, Prefix: prefix}
	}
	for _, tc := range []struct {
		name string
		h    ir.Header
		want []string
	}{
		{"a map under a prefix", header("t/x/Meta", "", new("X-Meta-")), nil},
		{"a map under the empty prefix", header("t/x/Meta", "", new("")), nil},
		{"a decorated map", header("t/x/Meta/decorated", "", new("X-Meta-")), nil},
		{"a dangling type", header("t/x/Missing", "", new("X-Meta-")), nil},
		{"one field of a model", header("t/x/Name", "X-Name", nil), nil},
		{"a model under a prefix", header("t/x/Name", "", new("X-Meta-")),
			[]string{"ir/prefix-header-not-map " + at + ".Type"}},
		{"a cycle of decorators", header("t/x/loop/a", "", new("")),
			[]string{"ir/prefix-header-not-map " + at + ".Type"}},
		{"a wire name beside the prefix", header("t/x/Meta", "X-Meta-", new("X-Meta-")),
			[]string{"ir/prefix-header-wire-name " + at + ".WireName"}},
		{"both", header("t/x/Name", "X-Meta-", new("")), []string{
			"ir/prefix-header-not-map " + at + ".Type", "ir/prefix-header-wire-name " + at + ".WireName",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, prefixHeaderViolations(tc.h))
		})
	}
}
