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
	header := ir.HTTPParamBinding{Param: "parameter/x/meta", Location: ir.HTTPLocationHeader, WireName: "X-Meta"}
	query := ir.HTTPParamBinding{Param: "parameter/x/meta", Location: ir.HTTPLocationQuery, WireName: "meta"}
	doc := opDoc(ir.Operation{
		ID:     "operation/x/S/op",
		Params: []ir.Parameter{{ID: "parameter/x/meta", Name: named("meta"), Type: ir.TypeRef{Target: target}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{
			{Method: "GET", URITemplate: "/a"},
			{Method: "GET", URITemplate: "/b", ParamBindings: []ir.HTTPParamBinding{header, query, pb}},
		}},
	})
	doc.Types = prefixHeaderTypes()
	var got []string
	for _, violation := range irverify.Verify(doc) {
		if strings.HasPrefix(violation.Code, "ir/prefix-binding-") {
			got = append(got, violation.Code+" "+violation.Path)
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
	const at = "document.Services[0].Groups[0].Operations[0].Bindings.HTTP[1].ParamBindings[2]"
	under := func(location ir.HTTPLocation, wire string, prefix *string) ir.HTTPParamBinding {
		return ir.HTTPParamBinding{Param: "parameter/x/meta", Location: location, WireName: wire, Prefix: prefix}
	}
	member := under(ir.HTTPLocationHeader, "", new("X-Meta-"))
	member.ParamPath = []ir.PropertyID{"property/x/meta"}
	stray := under(ir.HTTPLocationHeader, "", new("X-Meta-"))
	stray.Param = "parameter/x/other"
	for _, tc := range []struct {
		name   string
		target ir.TypeID
		pb     ir.HTTPParamBinding
		want   []string
	}{
		{"headers under a prefix", "type/x/Meta", under(ir.HTTPLocationHeader, "", new("X-Meta-")), nil},
		{"every query parameter", "type/x/Meta", under(ir.HTTPLocationQuery, "", new("")), nil},
		{"a decorated map", "type/x/Meta/decorated", under(ir.HTTPLocationHeader, "", new("")), nil},
		{"a dangling type", "type/x/Missing", under(ir.HTTPLocationQuery, "", new("")), nil},
		{"a nil type", "type/x/Nil", under(ir.HTTPLocationQuery, "", new("")), nil},
		{"one field of a model", "type/x/Name", under(ir.HTTPLocationHeader, "X-Name", nil), nil},
		{"a member of a model", "type/x/Name", member, nil},
		{"no parameter of the operation", "type/x/Name", stray, nil},
		{"a model under a prefix", "type/x/Name", under(ir.HTTPLocationHeader, "", new("X-Meta-")),
			[]string{"ir/prefix-binding-not-map " + at + ".Param"}},
		{"a cycle of decorators", "type/x/loop/a", under(ir.HTTPLocationQuery, "", new("")),
			[]string{"ir/prefix-binding-not-map " + at + ".Param"}},
		{"a wire name beside the prefix", "type/x/Meta", under(ir.HTTPLocationHeader, "X-Meta-", new("X-Meta-")),
			[]string{"ir/prefix-binding-wire-name " + at + ".WireName"}},
		{"a path", "type/x/Meta", under(ir.HTTPLocationPath, "", new("")),
			[]string{"ir/prefix-binding-location " + at + ".Location"}},
		{"a cookie", "type/x/Meta", under(ir.HTTPLocationCookie, "", new("c-")),
			[]string{"ir/prefix-binding-location " + at + ".Location"}},
		{"all three", "type/x/Name", under(ir.HTTPLocationBody, "meta", new("")), []string{
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
