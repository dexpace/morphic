// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// prefixBindingViolations returns what checkPrefixBindings reports, each
// prefixed by its code, for pb bound third on the second HTTP binding of an
// operation whose one parameter, param/x/meta, has type target.
func prefixBindingViolations(target ir.TypeID, pb ir.HTTPParamBinding) []string {
	header := ir.HTTPParamBinding{Param: "param/x/meta", Location: ir.HTTPLocationHeader, WireName: "X-Meta"}
	query := ir.HTTPParamBinding{Param: "param/x/meta", Location: ir.HTTPLocationQuery, WireName: "meta"}
	doc := opDoc(ir.Operation{
		ID:     "op/x/S/op",
		Params: []ir.Parameter{{ID: "param/x/meta", Name: named("meta"), Type: ir.TypeRef{Target: target}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{
			{Method: "GET", URITemplate: "/a"},
			{Method: "GET", URITemplate: "/b", ParamBindings: []ir.HTTPParamBinding{header, query, pb}},
		}},
	})
	doc.Types = prefixHeaderTypes()
	var got []string
	for _, v := range irverify.Verify(doc) {
		if strings.HasPrefix(v.Code, "ir/prefix-binding-") {
			got = append(got, v.Code+" "+v.Path)
		}
	}
	return got
}

// TestVerify_ABindingUnderAPrefixSpreadsAMapOverHeadersOrQuery holds a binding
// under a prefix, the empty one included, to a header or query location, to
// no wire name and, binding a whole parameter, to a type reaching a map
// through any decorators. It leaves a binding of one field, a member's, one
// naming no parameter, a dangling type and a nil one to what owns them
// (docs/ir/http-binding.md#prefix-bindings).
func TestVerify_ABindingUnderAPrefixSpreadsAMapOverHeadersOrQuery(t *testing.T) {
	t.Parallel()
	const at = "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[1].ParamBindings[2]"
	under := func(location ir.HTTPLocation, wire string, prefix *string) ir.HTTPParamBinding {
		return ir.HTTPParamBinding{Param: "param/x/meta", Location: location, WireName: wire, Prefix: prefix}
	}
	member := under(ir.HTTPLocationHeader, "", new("X-Meta-"))
	member.ParamPath = []ir.PropID{"p/x/meta"}
	stray := under(ir.HTTPLocationHeader, "", new("X-Meta-"))
	stray.Param = "param/x/other"
	for _, tc := range []struct {
		name   string
		target ir.TypeID
		pb     ir.HTTPParamBinding
		want   []string
	}{
		{"headers under a prefix", "t/x/Meta", under(ir.HTTPLocationHeader, "", new("X-Meta-")), nil},
		{"every query parameter", "t/x/Meta", under(ir.HTTPLocationQuery, "", new("")), nil},
		{"a decorated map", "t/x/Meta/decorated", under(ir.HTTPLocationHeader, "", new("")), nil},
		{"a dangling type", "t/x/Missing", under(ir.HTTPLocationQuery, "", new("")), nil},
		{"a nil type", "t/x/Nil", under(ir.HTTPLocationQuery, "", new("")), nil},
		{"one field of a model", "t/x/Name", under(ir.HTTPLocationHeader, "X-Name", nil), nil},
		{"a member of a model", "t/x/Name", member, nil},
		{"no parameter of the operation", "t/x/Name", stray, nil},
		{"a model under a prefix", "t/x/Name", under(ir.HTTPLocationHeader, "", new("X-Meta-")),
			[]string{"ir/prefix-binding-not-map " + at + ".Param"}},
		{"a cycle of decorators", "t/x/loop/a", under(ir.HTTPLocationQuery, "", new("")),
			[]string{"ir/prefix-binding-not-map " + at + ".Param"}},
		{"a wire name beside the prefix", "t/x/Meta", under(ir.HTTPLocationHeader, "X-Meta-", new("X-Meta-")),
			[]string{"ir/prefix-binding-wire-name " + at + ".WireName"}},
		{"a path", "t/x/Meta", under(ir.HTTPLocationPath, "", new("")),
			[]string{"ir/prefix-binding-location " + at + ".Location"}},
		{"a cookie", "t/x/Meta", under(ir.HTTPLocationCookie, "", new("c-")),
			[]string{"ir/prefix-binding-location " + at + ".Location"}},
		{"all three", "t/x/Name", under(ir.HTTPLocationBody, "meta", new("")), []string{
			"ir/prefix-binding-location " + at + ".Location",
			"ir/prefix-binding-not-map " + at + ".Param",
			"ir/prefix-binding-wire-name " + at + ".WireName",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, prefixBindingViolations(tc.target, tc.pb))
		})
	}
}
