// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement_test

import (
	"net/http"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/replacement"
	"github.com/dexpace/morphic/ir"
)

// inferring is a context under the default policy: the replacement policy on,
// and no key mapped to the target.
func inferring() lowering.Ctx {
	return lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{})
}

// says is a deprecation whose message is message.
func says(message string) *ir.Deprecation { return &ir.Deprecation{Message: message} }

// route is an operation id bound by method to path, deprecated by dep.
func route(id ir.OpID, method, path string, dep *ir.Deprecation) ir.Operation {
	return ir.Operation{ID: id, Deprecation: dep, Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{
		{Method: method, URITemplate: path},
	}}}
}

// routes is a document of one service holding ops.
func routes(ops ...ir.Operation) *ir.Document {
	return &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: ops}}}}}
}

// TestMessage_AnOperationsNamesItsReplacementByRoute pins how an operation's
// message names its replacement: by a method the document binds an operation
// with and a path template it declares, in a code span or out of one,
// punctuation after the path cut where it reads as written to nothing, all or
// none, the operation itself passed over and a repeat kept once. A word no
// binding with a path sends by, a webhook's method among them, is no method.
func TestMessage_AnOperationsNamesItsReplacementByRoute(t *testing.T) {
	t.Parallel()
	v2 := ir.EntityRef{Operation: "op/v2"}
	for _, tc := range []struct {
		message string
		want    []ir.EntityRef
	}{
		{"Use `GET /v2/zones` instead.", []ir.EntityRef{v2}},
		{"Deprecated in favor of the GET `/v2/zones` endpoint.", []ir.EntityRef{v2}},
		{"Use GET /v2/zones before 2026-11-28.", []ir.EntityRef{v2}},
		{"Use GET /v2/zones.", []ir.EntityRef{v2}},
		{"See (GET /v2/zones).", []ir.EntityRef{v2}},
		{"Use GET /v2/zones; GET /v1/zones goes away.", []ir.EntityRef{v2}},
		{"Use GET /v2/zones, or GET /v2/zones with a filter.", []ir.EntityRef{v2}},
		{"Use GET /v2/zones and POST /v2/zones.", []ir.EntityRef{v2, {Operation: "op/v2-post"}}},
		{"Use GET /v2/zones; FETCH /v2/zones is the hook's.", []ir.EntityRef{v2}},
		{"Use GET /v2/zones, as described at /docs.", []ir.EntityRef{v2}},
		{"GET requests go to GET /v2/zones.", []ir.EntityRef{v2}},
		{"Use GET /v2/zones or GET /v3/zones.", nil},
		{"Use GET /zones instead.", nil},
		{"Use FETCH /v2/zones instead.", nil},
		{"Use get /v2/zones instead.", nil},
		{"Use GET v2/zones instead.", nil},
		{"GET /v1/zones is gone.", nil},
		{"Use the `/v2/zones` endpoint.", nil},
		{"Use GET", nil},
		{"", nil},
	} {
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()
			doc := routes(route("op/v1", http.MethodGet, "/v1/zones", says(tc.message)),
				route("op/v2", http.MethodGet, "/v2/zones", nil), route("op/v2-post", http.MethodPost, "/v2/zones", nil),
				ir.Operation{ID: "op/hook", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "FETCH", WebhookName: "/v2/zones"}}}})
			assert.Empty(t, replacement.Resolve(inferring(), doc))
			op := doc.Services[0].Groups[0].Operations[0]
			assert.Equal(t, tc.want, op.Deprecation.ReplacedBy)
			assert.Equal(t, tc.want != nil, op.Provenance.Inferred == lowering.ReplacementInferenceHeuristic, "marked where filled")
		})
	}
}

// TestMessage_ARouteTwoOperationsShareNamesNone pins that a route bound to more
// than one operation names none of them, whichever is declared first.
func TestMessage_ARouteTwoOperationsShareNamesNone(t *testing.T) {
	t.Parallel()
	for _, first := range []bool{false, true} {
		a, b := route("op/a", http.MethodGet, "/v2", nil), route("op/b", http.MethodGet, "/v2", nil)
		if first {
			a, b = b, a
		}
		doc := routes(route("op/v1", http.MethodGet, "/v1", says("Use GET /v2.")), a, b)
		assert.Empty(t, replacement.Resolve(inferring(), doc))
		assert.Empty(t, doc.Services[0].Groups[0].Operations[0].Deprecation.ReplacedBy, "first %t", first)
	}
}

// TestMessage_AMembersAndAParametersNameSiblingsInCode pins how a property's
// and a parameter's message name their replacements: each code span names a
// member of the property's model, or a parameter of the operation, all or none,
// the node itself passed over.
func TestMessage_AMembersAndAParametersNameSiblingsInCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		message string
		want    []string
	}{
		{"Use `new` instead.", []string{"new"}},
		{"Use `new` and `other` for finer control.", []string{"new", "other"}},
		{"Renamed from `old` to `new`.", []string{"new"}},
		{"Use `new` instead; see `docs`.", nil},
		{"Use new instead.", nil},
		{"Use ``new`` instead.", []string{"new"}},
		{"Use `old` no more.", nil},
	} {
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()
			model := &ir.Model{ID: "t/m", Properties: []ir.Property{
				{ID: "p/old", WireName: "old", Deprecation: says(tc.message)},
				{ID: "p/new", WireName: "new"}, {ID: "p/other", WireName: "other"},
			}}
			op := ir.Operation{ID: "op/a",
				Params: []ir.Parameter{
					{ID: "param/old", Name: ir.Naming{Source: "old"}, Deprecation: says(tc.message)},
					{ID: "param/new", Name: ir.Naming{Source: "new"}}, {ID: "param/other", Name: ir.Naming{Source: "other"}},
				},
				Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
					{Param: "param/old", Location: ir.HTTPLocationQuery}, {Param: "param/new", Location: ir.HTTPLocationQuery},
					{Param: "param/other", Location: ir.HTTPLocationQuery},
				}}}},
			}
			doc := routes(op)
			doc.Types = ir.TypeRegistry{"t/m": model}
			assert.Empty(t, replacement.Resolve(inferring(), doc))

			members := make([]ir.EntityRef, 0, len(tc.want))
			params := make([]ir.EntityRef, 0, len(tc.want))
			for _, name := range tc.want {
				members = append(members, ir.EntityRef{Property: ir.PropID("p/" + name)})
				params = append(params, ir.EntityRef{Param: ir.ParamID("param/" + name)})
			}
			if len(tc.want) == 0 {
				members, params = nil, nil
			}
			assert.Equal(t, members, model.Properties[0].Deprecation.ReplacedBy, "the member")
			assert.Equal(t, params, doc.Services[0].Groups[0].Operations[0].Params[0].Deprecation.ReplacedBy, "the parameter")
		})
	}
}

// TestMessage_YieldsToWhatTheDocumentSays pins the precedence: a replacement a
// mapped key names wins, a mapped key that names nothing still keeps the
// message from speaking, and a node that is not deprecated, or whose
// deprecation has no message, takes none; with the policy off none is read.
func TestMessage_YieldsToWhatTheDocumentSays(t *testing.T) {
	t.Parallel()
	build := func(own ir.Unmodeled, dep *ir.Deprecation) *ir.Document {
		v1 := route("op/v1", http.MethodGet, "/v1", dep)
		v1.Unmodeled = own
		v2 := route("op/v2", http.MethodGet, "/v2", nil)
		v2.Name = ir.Naming{Source: "getV2"}
		v3 := route("op/v3", http.MethodGet, "/v3", nil)
		return routes(v1, v2, v3)
	}
	replaced := func(doc *ir.Document) []ir.EntityRef {
		if dep := doc.Services[0].Groups[0].Operations[0].Deprecation; dep != nil {
			return dep.ReplacedBy
		}
		return nil
	}
	off := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}},
		lowering.Policy{ReplacementInference: lowering.ReplacementInference{Disabled: true}})
	mappedOff := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{
		Promotions:           replacedBy(),
		ReplacementInference: lowering.ReplacementInference{Disabled: true},
	})
	for _, tc := range []struct {
		name string
		c    lowering.Ctx
		own  ir.Unmodeled
		dep  *ir.Deprecation
		want []ir.EntityRef
	}{
		{"a key naming a replacement", mapped(), names(`"getV2"`), says("Use GET /v3."), []ir.EntityRef{{Operation: "op/v2"}}},
		{"a key naming nothing", mapped(), names(`"gone"`), says("Use GET /v3."), nil},
		{"no key", mapped(), nil, says("Use GET /v3."), []ir.EntityRef{{Operation: "op/v3"}}},
		{"no deprecation", inferring(), nil, nil, nil},
		{"no message", inferring(), nil, &ir.Deprecation{}, nil},
		{"the policy off", off, nil, says("Use GET /v3."), nil},
		{"the policy off beside a mapping", mappedOff, nil, says("Use GET /v3."), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := build(tc.own, tc.dep)
			replacement.Resolve(tc.c, doc)
			assert.Equal(t, tc.want, replaced(doc))
		})
	}
}

// TestCodeSpans_ReadsCommonMarksSpans pins the spans a message holds, as
// CommonMark reads them: a run of backticks closed by the next run as long,
// a run nothing closes kept as text, a line ending read as a space and one
// space at each end of content not all spaces stripped.
func TestCodeSpans_ReadsCommonMarksSpans(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"`a` and `b`", []string{"a", "b"}},
		{"`` a ` b ``", []string{"a ` b"}},
		{"``a`", nil},
		{"` `", []string{" "}},
		{"`  `", []string{"  "}},
		{"` a`", []string{" a"}},
		{"`a\nb`", []string{"a b"}},
		{"`a\r\nb`", []string{"a b"}},
		{"unclosed `a", nil},
		{"``", nil},
		{"`a``b`", []string{"a``b"}},
		{"no code", nil},
		{"`a``b```c", nil},
		{"``a`b``", []string{"a`b"}},
		{"``a `b`", []string{"b"}},
		{"`a` ``b`` `c`", []string{"a", "b", "c"}},
	} {
		assert.Equal(t, tc.want, replacement.CodeSpans(tc.text), "%q", tc.text)
	}
}
