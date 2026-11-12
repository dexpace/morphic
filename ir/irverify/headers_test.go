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

// prefixHeaderTypes are the types the headers of this file's tests name: a
// map, an anonymous model decorating it, a model, two anonymous models
// decorating each other, and a nil entry.
func prefixHeaderTypes() ir.TypeRegistry {
	str := ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString)}
	decorates := func(id, base ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Anonymous: true, Base: &ir.TypeRef{Target: base}}
	}
	types := ir.TypeRegistry{"t/x/Nil": (*ir.Model)(nil)}
	for _, td := range []ir.TypeDef{
		&ir.MapT{ID: "t/x/Meta", Name: named("meta"), Value: str},
		decorates("t/x/Meta/decorated", "t/x/Meta"),
		&ir.Model{ID: "t/x/Name", Name: named("name")},
		decorates("t/x/loop/a", "t/x/loop/b"),
		decorates("t/x/loop/b", "t/x/loop/a"),
	} {
		types[td.Common().ID] = td
	}
	return types
}

// prefixHeaderViolations returns the paths checkPrefixHeaders reports for a
// response declaring h, each prefixed by its code.
func prefixHeaderViolations(h ir.Header) []string {
	doc := opDoc(ir.Operation{ID: "op/x/S/op", Responses: []ir.Response{{Headers: []ir.Header{h}}}})
	doc.Types = prefixHeaderTypes()
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
// to no wire name, and leaves a header of one field, a dangling type and a nil
// one to the checks that own them (docs/ir/responses.md#prefix-headers).
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
		{"a nil type", header("t/x/Nil", "", new("X-Meta-")), nil},
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

// TestVerify_AHeaderUnderAPrefixIsCheckedWhereverAHeaderIs holds one header
// under a prefix whose type is no map at every position a header can take, so
// a position headerPath leaves out passes unread and fails here.
func TestVerify_AHeaderUnderAPrefixIsCheckedWhereverAHeaderIs(t *testing.T) {
	t.Parallel()
	bad := func() []ir.Header {
		return []ir.Header{{ID: "p/x/h", Name: named("meta"), Type: ir.TypeRef{Target: "t/x/Name"}, Prefix: new("")}}
	}
	parts := func() *ir.Payload {
		return &ir.Payload{Contents: []ir.Content{{
			MediaType: "multipart/mixed", Type: ir.TypeRef{Target: "t/x/Name"},
			ItemEncoding: &ir.PartEncoding{Headers: bad()},
			Encoding:     map[ir.PropID]ir.PartEncoding{"p/x/part": {Headers: bad()}},
		}}}
	}
	doc := opDoc(ir.Operation{
		ID: "op/x/S/op", Request: parts(),
		Responses: []ir.Response{{Headers: bad(), Payload: parts()}},
		Default:   &ir.Response{Headers: bad(), Payload: parts()},
		Errors:    []ir.ErrorCase{{Headers: bad(), Payload: parts()}},
	})
	doc.Services[0].CommonErrors = []ir.ErrorCase{{Headers: bad(), Payload: parts()}}
	doc.Messages = map[ir.MessageID]ir.Message{"m/x": {ID: "m/x", Name: named("m"), Payload: *parts()}}
	doc.Types = prefixHeaderTypes()

	const op = "doc.Services[0].Groups[0].Operations[0]"
	payloads := []string{
		op + ".Request", op + ".Responses[0].Payload", op + ".Default.Payload", op + ".Errors[0].Payload",
		"doc.Services[0].CommonErrors[0].Payload", "doc.Messages[m/x].Payload",
	}
	holders := []string{op + ".Responses[0]", op + ".Default", op + ".Errors[0]", "doc.Services[0].CommonErrors[0]"}
	want := make([]string, 0, 2*len(payloads)+len(holders))
	for _, payload := range payloads {
		want = append(want, payload+".Contents[0].ItemEncoding.Headers[0].Type",
			payload+".Contents[0].Encoding[p/x/part].Headers[0].Type")
	}
	for _, holder := range holders {
		want = append(want, holder+".Headers[0].Type")
	}
	assert.ElementsMatch(t, want, violationPaths(irverify.Verify(doc), "ir/prefix-header-not-map"))
}
