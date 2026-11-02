// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// anonAny is an anonymous type holding nothing, keyed only by a position.
func anonAny(id ir.TypeID) *ir.Any {
	return &ir.Any{TypeCommon: ir.TypeCommon{ID: id, Anonymous: true}}
}

// namedOp is an operation the source names, bound to GET path.
func namedOp(id ir.OpID, name, path string) ir.Operation {
	return ir.Operation{
		ID: id, Name: ir.Naming{Source: name},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "GET", URITemplate: path}}},
	}
}

// opsDoc is a document of types whose one service groups ops.
func opsDoc(ops []ir.Operation, defs ...ir.TypeDef) *ir.Document {
	doc := irtest.DocOf(defs...)
	doc.Services = []ir.Service{{ID: "s/test/0", Groups: []ir.OperationGroup{{ID: "g/default", Operations: ops}}}}
	return doc
}

// keysOf returns the key of each id, "" for one that has none.
func keysOf(facts *irfacts.Index, ids ...string) map[string]string {
	got := map[string]string{}
	for _, id := range ids {
		key, ok := facts.Correlation(id)
		if ok != (key != "") {
			key = "inconsistent"
		}
		got[id] = key
	}
	return got
}

func TestCorrelation_KeysAnOperationByNameElseRoute(t *testing.T) {
	t.Parallel()
	unnamed := namedOp("op/test/b", "", "/b/{id}/c{?q}")
	hook := ir.Operation{ID: "op/test/hook", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{
		{Method: "POST", Inbound: ir.HTTPInboundWebhook, WebhookName: "created"},
	}}}
	open := namedOp("op/test/open", "", "/x/{y")
	bare := ir.Operation{ID: "op/test/bare"}
	doc := opsDoc([]ir.Operation{namedOp("op/test/a", "getA", "/a"), unnamed, hook, open, bare})
	got := keysOf(irfacts.New(doc), "op/test/a", "op/test/b", "op/test/hook", "op/test/open", "op/test/bare")
	want := map[string]string{
		"op/test/a":    "op:getA",
		"op/test/b":    "route:GET ~1b~1{}~1c{}",
		"op/test/hook": "webhook:POST created",
		"op/test/open": "route:GET ~1x~1{y",
		"op/test/bare": "",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysAParameterByWhereItTravels(t *testing.T) {
	t.Parallel()
	op := namedOp("op/test/a", "getA", "/a/{Fallback}")
	op.Params = []ir.Parameter{
		{ID: "param/test/q", Name: ir.Naming{Source: "q"}, Type: ir.TypeRef{Target: "t/anon/q"}},
		{ID: "param/test/h", Name: ir.Naming{Source: "X-Trace"}},
		{ID: "param/test/f", Name: ir.Naming{Source: "Fallback"}},
		{ID: "param/test/host", Name: ir.Naming{Source: "region"}},
		{ID: "param/test/none"},
	}
	op.Bindings.HTTP[0].ParamBindings = []ir.HTTPParamBinding{
		{Param: "param/test/q", Location: ir.HTTPLocationQuery, WireName: "q"},
		{Param: "param/test/h", Location: ir.HTTPLocationHeader, WireName: "X-Trace"},
		{Param: "param/test/f", Location: ir.HTTPLocationPath},
		{Param: "param/test/host", Location: ir.HTTPLocationHost, WireName: "region"},
	}
	doc := opsDoc([]ir.Operation{op}, anonAny("t/anon/q"))
	got := keysOf(irfacts.New(doc), "param/test/q", "param/test/h", "param/test/f", "param/test/host",
		"param/test/none", "t/anon/q")
	want := map[string]string{
		"param/test/q":    "op:getA/param:query q",
		"param/test/h":    "op:getA/param:header x-trace",
		"param/test/f":    "op:getA/param:path Fallback",
		"param/test/host": "op:getA/arg:region",
		"param/test/none": "",
		"t/anon/q":        "op:getA/param:query q/schema",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysWhatAnOperationNamesByItsPosition(t *testing.T) {
	t.Parallel()
	op := namedOp("op/test/a", "getA", "/a")
	op.Request = &ir.Payload{Contents: []ir.Content{{
		MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/req"},
		Item: &ir.TypeRef{Target: "t/anon/item"}, File: &ir.FileInfo{Contents: &ir.TypeRef{Target: "t/anon/file"}},
		ItemEncoding: &ir.PartEncoding{Headers: []ir.Header{{Property: ir.Property{ID: "p/test/ih", Name: ir.Naming{Source: "X-I"}}}}},
	}, {MediaType: "text/plain", Type: ir.TypeRef{Target: "t/anon/text"}}}}
	op.Responses = []ir.Response{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}, {From: 400, To: 499}}},
		Payload:    &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/ok"}}}},
		Headers: []ir.Header{{Property: ir.Property{
			ID: "p/test/rh", Name: ir.Naming{Source: "Retry-After"}, Type: ir.TypeRef{Target: "t/anon/rh"},
			Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t/anon/rhd"}},
		}}},
	}}
	op.Default = &ir.Response{
		Payload: &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/def"}}}},
		Headers: []ir.Header{{Property: ir.Property{ID: "p/test/dh", WireName: "X-D"}}},
	}
	op.Errors = []ir.ErrorCase{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 500, To: 599}}},
		Payload:    &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/err"}}}},
	}}
	op.RequestStream = &ir.StreamDetail{Events: &ir.TypeRef{Target: "t/anon/in"}}
	op.ResponseStream = &ir.StreamDetail{Initial: &ir.TypeRef{Target: "t/anon/init"}}
	op.Bindings.RPC = &ir.RPCBinding{InputType: &ir.TypeRef{Target: "t/anon/rpc"}}
	op.LongRunning = &ir.LongRunning{PollingType: &ir.TypeRef{Target: "t/anon/poll"}, FinalType: &ir.TypeRef{Target: "t/anon/final"}}
	ids := []ir.TypeID{
		"t/anon/req", "t/anon/item", "t/anon/file", "t/anon/text", "t/anon/ok", "t/anon/rh", "t/anon/rhd",
		"t/anon/def", "t/anon/err", "t/anon/in", "t/anon/init", "t/anon/rpc", "t/anon/poll", "t/anon/final",
	}
	defs := make([]ir.TypeDef, 0, len(ids))
	for _, id := range ids {
		defs = append(defs, anonAny(id))
	}
	doc := opsDoc([]ir.Operation{op}, defs...)
	keys := []string{"p/test/ih", "p/test/rh", "p/test/dh"}
	for _, id := range ids {
		keys = append(keys, string(id))
	}
	got := keysOf(irfacts.New(doc), keys...)
	want := map[string]string{
		"t/anon/req":   "op:getA/request/content:application~1json",
		"t/anon/item":  "op:getA/request/content:application~1json/item",
		"t/anon/file":  "op:getA/request/content:application~1json/file",
		"p/test/ih":    "op:getA/request/content:application~1json/item/header:x-i",
		"t/anon/text":  "op:getA/request/content:text~1plain",
		"t/anon/ok":    "op:getA/response:200,400-499/content:application~1json",
		"p/test/rh":    "op:getA/response:200,400-499/header:retry-after",
		"t/anon/rh":    "op:getA/response:200,400-499/header:retry-after/type",
		"t/anon/rhd":   "op:getA/response:200,400-499/header:retry-after/decoded",
		"t/anon/def":   "op:getA/default/content:application~1json",
		"p/test/dh":    "op:getA/default/header:x-d",
		"t/anon/err":   "op:getA/error:500-599/content:application~1json",
		"t/anon/in":    "op:getA/requestStream/events",
		"t/anon/init":  "op:getA/responseStream/initial",
		"t/anon/rpc":   "op:getA/input",
		"t/anon/poll":  "op:getA/polling",
		"t/anon/final": "op:getA/final",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysTheServiceLayerByName(t *testing.T) {
	t.Parallel()
	doc := opsDoc([]ir.Operation{namedOp("op/test/b", "b", "/b"), namedOp("op/test/a", "a", "/a")},
		anonAny("t/anon/common"), anonAny("t/anon/cred"))
	doc.Services[0].Name = ir.Naming{Source: "Svc"}
	doc.Services[0].Servers = []ir.ServerID{"server/test/root", "server/test/missing"}
	doc.Services[0].CommonErrors = []ir.ErrorCase{{Payload: &ir.Payload{Contents: []ir.Content{
		{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/common"}},
	}}}}
	doc.Services[0].Groups[0].Operations[0].Servers = []ir.ServerID{"server/test/path"}
	doc.Services[0].Groups[0].Operations[1].Servers = []ir.ServerID{"server/test/path"}
	doc.Services = append(doc.Services, ir.Service{ID: "s/test/1"})
	doc.Servers = map[ir.ServerID]ir.Server{
		"server/test/root": {ID: "server/test/root", URLTemplate: "https://api.example.com/v1"},
		"server/test/path": {ID: "server/test/path", URLTemplate: "https://{region}.example.com"},
	}
	doc.Auth = map[ir.AuthID]ir.AuthScheme{
		"auth/test/key":  {ID: "auth/test/key", Name: ir.Naming{Source: "api_key"}, Credentials: &ir.TypeRef{Target: "t/anon/cred"}},
		"auth/test/anon": {ID: "auth/test/anon"},
	}
	got := keysOf(irfacts.New(doc), "s/test/0", "s/test/1", "t/anon/common", "server/test/root",
		"server/test/path", "auth/test/key", "auth/test/anon", "t/anon/cred", "g/default")
	want := map[string]string{
		"s/test/0":         "service:Svc",
		"s/test/1":         "",
		"t/anon/common":    "service:Svc/error:/content:application~1json",
		"server/test/root": "service:Svc/server:https:~1~1api.example.com~1v1",
		"server/test/path": "op:a/server:https:~1~1{region}.example.com",
		"auth/test/key":    "auth:api_key",
		"auth/test/anon":   "",
		"t/anon/cred":      "auth:api_key/credentials",
		"g/default":        "group:g~1default",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysAGroupOnlyWithinTheDepthBound(t *testing.T) {
	t.Parallel()
	ids := make([]string, ir.MaxGroupDepth+2)
	var nested []ir.OperationGroup
	for depth := len(ids) - 1; depth >= 0; depth-- {
		ids[depth] = "g/test/" + string(rune('a'+depth%26)) + string(rune('a'+depth/26))
		nested = []ir.OperationGroup{{ID: ir.GroupID(ids[depth]), Groups: nested}}
	}
	doc := irtest.DocOf()
	doc.Services = []ir.Service{{Groups: append(nested, ir.OperationGroup{})}}
	facts := irfacts.New(doc)
	_, deepest := facts.Correlation(ids[len(ids)-1])
	_, bounded := facts.Correlation(ids[ir.MaxGroupDepth])
	assert.False(t, deepest, "a group past the bound is not read")
	assert.True(t, bounded, "a group at the bound is read")
}

func TestCorrelation_KeysACallbackThroughTheOperationDeclaringIt(t *testing.T) {
	t.Parallel()
	parent := namedOp("op/test/sub", "subscribe", "/sub")
	parent.Bindings.HTTP[0].Callbacks = []ir.Callback{{
		Name: "on/event", Expression: "{$request.body#/url}",
		Operations: []ir.OpID{"op/test/cb", "op/test/named", "op/test/nobinding", "op/test/missing"},
	}}
	cb := ir.Operation{ID: "op/test/cb", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "POST"}}}}
	named := namedOp("op/test/named", "onEvent", "{$request.body#/url}")
	noBinding := ir.Operation{ID: "op/test/nobinding"}
	// An unnamed parent's route sorts after its callback's would, so a callback
	// keyed by its own route would settle first.
	unnamedParent := namedOp("op/test/sub2", "", "/sub2")
	unnamedParent.Bindings.HTTP[0].Method = "POST"
	unnamedParent.Bindings.HTTP[0].Callbacks = []ir.Callback{{Name: "on", Expression: "{$url}", Operations: []ir.OpID{"op/test/cb2"}}}
	cb2 := ir.Operation{ID: "op/test/cb2", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "POST", URITemplate: "{$url}"}}}}
	doc := opsDoc([]ir.Operation{parent, cb, named, noBinding, unnamedParent, cb2})
	got := keysOf(irfacts.New(doc), "op/test/cb", "op/test/named", "op/test/nobinding", "op/test/cb2")
	want := map[string]string{
		"op/test/cb":        "op:subscribe/callback:on~1event/method:POST",
		"op/test/named":     "op:onEvent",
		"op/test/nobinding": "",
		"op/test/cb2":       "route:POST ~1sub2/callback:on/method:POST",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysWhatATypeHolds(t *testing.T) {
	t.Parallel()
	holder := irtest.ModelOf("t/test/Holder", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "Holder"}
		m.Namespace = []string{"acme", "v1"}
		m.Properties = []ir.Property{{
			ID: "p/test/a", Name: ir.Naming{Source: "a"}, WireName: "A", Type: ir.TypeRef{Target: "t/anon/a"},
			Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t/anon/ad"}},
			Args:     []ir.Parameter{{ID: "param/test/arg", Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: "t/anon/arg"}}},
		}, {ID: "p/test/l", Name: ir.Naming{Source: "l"}, Type: ir.TypeRef{Target: "t/anon/list"}},
			{ID: "p/test/gone", Name: ir.Naming{Source: "gone"}, Type: ir.TypeRef{Target: "t/missing"}},
			{ID: "p/test/nil", Name: ir.Naming{Source: "nil"}, Type: ir.TypeRef{Target: "t/test/nil"}}}
		m.AdditionalProps = &ir.AdditionalProps{
			Value: &ir.TypeRef{Target: "t/anon/av"}, Key: &ir.TypeRef{Target: "t/anon/ak"},
			Patterns: []ir.PatternProps{{Pattern: "^x-/", Value: ir.TypeRef{Target: "t/anon/pv"}}},
		}
	})
	list := &ir.List{TypeCommon: ir.TypeCommon{ID: "t/anon/list", Anonymous: true}, Elem: ir.TypeRef{Target: "t/anon/map"},
		Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t/anon/ld"}}}
	mapT := &ir.MapT{TypeCommon: ir.TypeCommon{ID: "t/anon/map", Anonymous: true},
		Key: &ir.TypeRef{Target: "t/anon/mk"}, Value: ir.TypeRef{Target: "t/anon/tuple"}}
	tuple := &ir.Tuple{TypeCommon: ir.TypeCommon{ID: "t/anon/tuple", Anonymous: true},
		Elems: []ir.TypeRef{{Target: "t/anon/e0"}, {Target: "t/anon/scalar"}}, Rest: &ir.TypeRef{Target: "t/anon/rest"}}
	scalar := &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/anon/scalar", Anonymous: true},
		Base: &ir.TypeRef{Target: "t/anon/base"}, Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t/anon/sd"}}}
	defs := []ir.TypeDef{holder, list, mapT, tuple, scalar, &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/prim/string"}, Prim: ir.PrimString}}
	ids := []string{"t/anon/a", "t/anon/ad", "t/anon/arg", "t/anon/av", "t/anon/ak", "t/anon/pv", "t/anon/ld",
		"t/anon/mk", "t/anon/e0", "t/anon/rest", "t/anon/base", "t/anon/sd"}
	for _, id := range ids {
		defs = append(defs, anonAny(ir.TypeID(id)))
	}
	var typedNil *ir.List
	doc := irtest.DocOf(defs...)
	doc.Types["t/test/nil"] = typedNil
	got := keysOf(irfacts.New(doc), append(ids, "t/test/Holder", "p/test/a", "param/test/arg", "p/test/gone",
		"p/test/l", "t/anon/list", "t/anon/map", "t/anon/tuple", "t/anon/scalar", "t/prim/string")...)
	h := "ns:acme/ns:v1/type:Holder"
	want := map[string]string{
		"t/test/Holder": h, "t/prim/string": "prim:string",
		"p/test/a": h + "/prop:A", "t/anon/a": h + "/prop:A/type", "t/anon/ad": h + "/prop:A/decoded",
		"param/test/arg": h + "/prop:A/arg:first", "t/anon/arg": h + "/prop:A/arg:first/schema",
		"t/anon/av": h + "/additional", "t/anon/ak": h + "/additionalKey", "t/anon/pv": h + "/pattern:^x-~1",
		"p/test/gone": h + "/prop:gone",
		"p/test/l":    h + "/prop:l", "t/anon/list": h + "/prop:l/type", "t/anon/ld": h + "/prop:l/type/decoded",
		"t/anon/map": h + "/prop:l/type/elem", "t/anon/mk": h + "/prop:l/type/elem/key",
		"t/anon/tuple": h + "/prop:l/type/elem/value", "t/anon/e0": h + "/prop:l/type/elem/value/elem:0",
		"t/anon/scalar": h + "/prop:l/type/elem/value/elem:1", "t/anon/rest": h + "/prop:l/type/elem/value/rest",
		"t/anon/base": h + "/prop:l/type/elem/value/elem:1/base", "t/anon/sd": h + "/prop:l/type/elem/value/elem:1/decoded",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysAnEnumMemberBeneathItsEnum(t *testing.T) {
	t.Parallel()
	enum := &ir.Enum{TypeCommon: ir.TypeCommon{ID: "t/test/Color", Name: ir.Naming{Source: "Color"}},
		Members: []ir.EnumMember{{ID: "e/test/Color/s:red"}, {ID: "e/other/Color/s:blue"}}}
	got := keysOf(irfacts.New(irtest.DocOf(enum)), "e/test/Color/s:red", "e/other/Color/s:blue")
	want := map[string]string{"e/test/Color/s:red": "type:Color/member:s:red", "e/other/Color/s:blue": ""}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysAModelsInPlaceConjunctsAsItsOwn(t *testing.T) {
	t.Parallel()
	base := irtest.ModelOf("t/test/N", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "N"}
		m.Properties = []ir.Property{{ID: "p/test/n", Name: ir.Naming{Source: "n"}}}
	})
	inner := irtest.ModelOf("t/anon/c3", func(m *ir.Model) {
		m.Anonymous = true
		m.Properties = []ir.Property{{ID: "p/test/z", Name: ir.Naming{Source: "z"}}}
		m.Mixins = irtest.RefsTo("t/anon/c2")
	})
	c1 := irtest.ModelOf("t/anon/c1", func(m *ir.Model) {
		m.Anonymous = true
		m.Properties = []ir.Property{{ID: "p/test/x", Name: ir.Naming{Source: "x"}}, {ID: "p/test/y1", Name: ir.Naming{Source: "y"}}}
	})
	c2 := irtest.ModelOf("t/anon/c2", func(m *ir.Model) {
		m.Anonymous = true
		m.Mixins = irtest.RefsTo("t/anon/c3", "t/missing", "t/anon/c3")
		m.AdditionalProps = &ir.AdditionalProps{Value: &ir.TypeRef{Target: "t/anon/extra"}}
	})
	solo := irtest.ModelOf("t/anon/solo", func(m *ir.Model) {
		m.Anonymous = true
		m.Properties = []ir.Property{{ID: "p/test/q", Name: ir.Naming{Source: "q"}}}
	})
	m := irtest.ModelOf("t/test/M", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "M"}
		m.Base = &ir.TypeRef{Target: "t/test/N"}
		m.Mixins = irtest.RefsTo("t/anon/c1", "t/anon/c2")
		m.Properties = []ir.Property{{ID: "p/test/y2", Name: ir.Naming{Source: "y"}}}
	})
	single := irtest.ModelOf("t/test/S", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "S"}
		m.Mixins = irtest.RefsTo("t/anon/solo")
	})
	var typedNil *ir.Model
	doc := irtest.DocOf(base, inner, c1, c2, solo, m, single, anonAny("t/anon/extra"))
	doc.Types["t/test/nil"] = typedNil
	got := keysOf(irfacts.New(doc), "p/test/n", "p/test/x", "p/test/y1", "p/test/y2", "p/test/z",
		"t/anon/c1", "t/anon/c2", "t/anon/c3", "t/anon/extra", "t/anon/solo", "p/test/q")
	want := map[string]string{
		"p/test/n": "type:N/prop:n", "p/test/x": "type:M/prop:x", "p/test/y1": "", "p/test/y2": "type:M/prop:y",
		"p/test/z": "type:M/prop:z", "t/anon/c1": "", "t/anon/c2": "", "t/anon/c3": "",
		"t/anon/extra": "type:M/additional", "t/anon/solo": "type:S/allOf", "p/test/q": "type:S/prop:q",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_KeysAVariantsTypeByWhatNamesTheVariant(t *testing.T) {
	t.Parallel()
	mapped := &ir.Union{TypeCommon: ir.TypeCommon{ID: "t/test/Mapped", Name: ir.Naming{Source: "Mapped"}},
		WireTagged: true,
		Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{
			"b": "t/anon/b", "a2": "t/anon/a", "a": "t/anon/a",
		}},
		Variants: []ir.Variant{
			{Type: ir.TypeRef{Target: "t/anon/a"}}, {Type: ir.TypeRef{Target: "t/anon/b"}},
			{Name: ir.Naming{Source: "c"}, Type: ir.TypeRef{Target: "t/anon/c"}}, {Type: ir.TypeRef{Target: "t/test/Named"}},
		}}
	keyed := &ir.Union{TypeCommon: ir.TypeCommon{ID: "t/test/Keyed", Name: ir.Naming{Source: "Keyed"}}, WireTagged: true,
		Variants: []ir.Variant{
			{WireName: "w", Type: ir.TypeRef{Target: "t/anon/w"}}, {Name: ir.Naming{Source: "n"}, Type: ir.TypeRef{Target: "t/anon/n"}},
			{Type: ir.TypeRef{Target: "t/anon/none"}},
		}}
	untagged := &ir.Union{TypeCommon: ir.TypeCommon{ID: "t/test/Plain", Name: ir.Naming{Source: "Plain"}},
		Variants: []ir.Variant{
			{Name: ir.Naming{Source: "v"}, Type: ir.TypeRef{Target: "t/anon/v"}}, {Type: ir.TypeRef{Target: "t/anon/bare"}},
		}}
	named := irtest.ModelOf("t/test/Named", func(m *ir.Model) { m.Name = ir.Naming{Source: "Named"} })
	defs := []ir.TypeDef{mapped, keyed, untagged, named}
	ids := []string{"t/anon/a", "t/anon/b", "t/anon/c", "t/anon/w", "t/anon/n", "t/anon/none", "t/anon/v", "t/anon/bare"}
	for _, id := range ids {
		defs = append(defs, anonAny(ir.TypeID(id)))
	}
	got := keysOf(irfacts.New(irtest.DocOf(defs...)), append(ids, "t/test/Named")...)
	want := map[string]string{
		"t/anon/a": "type:Mapped/tag:a", "t/anon/b": "type:Mapped/tag:b", "t/anon/c": "type:Mapped/variant:c",
		"t/anon/w": "type:Keyed/tag:w", "t/anon/n": "type:Keyed/tag:n", "t/anon/none": "",
		"t/anon/v": "type:Plain/variant:v", "t/anon/bare": "", "t/test/Named": "type:Named",
	}
	assert.Empty(t, cmp.Diff(want, got))
}

func TestCorrelation_ASharedKeyKeysNeitherEntityNorWhatTheyName(t *testing.T) {
	t.Parallel()
	pet := func(id ir.TypeID, prop ir.PropID, held ir.TypeID) *ir.Model {
		return irtest.ModelOf(id, func(m *ir.Model) {
			m.Name = ir.Naming{Source: "Pet"}
			m.Properties = []ir.Property{{ID: prop, Name: ir.Naming{Source: string(prop)}, Type: ir.TypeRef{Target: held}}}
		})
	}
	doc := irtest.DocOf(pet("t/one/Pet", "p/one/id", "t/anon/one"), pet("t/two/Pet", "p/two/tag", "t/anon/two"),
		anonAny("t/anon/one"), anonAny("t/anon/two"), irtest.ModelOf("t/test/Anon", func(m *ir.Model) {}))
	facts := irfacts.New(doc)
	got := keysOf(facts, "t/one/Pet", "t/two/Pet", "p/one/id", "p/two/tag", "t/anon/one", "t/test/Anon")
	for id, key := range got {
		assert.Empty(t, key, id)
	}
	_, found := facts.Correlated("type:Pet")
	assert.False(t, found, "a key two entities share names neither")
}

func TestCorrelation_TakesTheLeastKeyAmongThePositionsNamingAType(t *testing.T) {
	t.Parallel()
	withParam := func(id ir.OpID, name string) ir.Operation {
		op := namedOp(id, name, "/"+name)
		op.Params = []ir.Parameter{{ID: ir.ParamID("param/test/" + name), Name: ir.Naming{Source: "p"}, Type: ir.TypeRef{Target: "t/anon/shared"}}}
		op.Request = &ir.Payload{Contents: []ir.Content{
			{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/twice"}},
			{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/twice"}},
		}}
		return op
	}
	pair := &ir.MapT{TypeCommon: ir.TypeCommon{ID: "t/test/Pair", Name: ir.Naming{Source: "Pair"}},
		Key: &ir.TypeRef{Target: "t/anon/kv"}, Value: ir.TypeRef{Target: "t/anon/kv"}}
	defs := []ir.TypeDef{anonAny("t/anon/shared"), anonAny("t/anon/twice"), anonAny("t/anon/kv"), pair}
	for _, ops := range [][]ir.Operation{
		{withParam("op/test/b", "b"), withParam("op/test/a", "a")},
		{withParam("op/test/a", "a"), withParam("op/test/b", "b")},
	} {
		facts := irfacts.New(opsDoc(ops, defs...))
		got := keysOf(facts, "t/anon/shared", "t/anon/twice", "t/anon/kv")
		want := map[string]string{
			"t/anon/shared": "op:a/arg:p/schema",
			"t/anon/twice":  "op:a/request/content:application~1json",
			"t/anon/kv":     "type:Pair/key",
		}
		assert.Empty(t, cmp.Diff(want, got))
		id, ok := facts.Correlated("op:a/arg:p/schema")
		assert.True(t, ok)
		assert.Equal(t, "t/anon/shared", id)
	}
}

func TestCorrelation_ADocumentWithNothingNamedKeysNothing(t *testing.T) {
	t.Parallel()
	var zero irfacts.Index
	_, fromZero := zero.Correlation("t/anon/x")
	_, fromNil := irfacts.New(nil).Correlated("type:X")
	var typedNil *ir.Model
	doc := irtest.DocOf(irtest.ModelOf("t/test/M", func(m *ir.Model) {
		m.Properties = []ir.Property{{ID: "p/test/a", Type: ir.TypeRef{Target: "t/test/nil"}}}
	}))
	doc.Types["t/test/nil"] = typedNil
	doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{}}}}
	facts := irfacts.New(doc)
	got := keysOf(facts, "t/test/M", "p/test/a", "t/test/nil")
	assert.False(t, fromZero)
	assert.False(t, fromNil)
	assert.True(t, slices.Equal([]string{"", "", ""}, []string{got["t/test/M"], got["p/test/a"], got["t/test/nil"]}))
}
