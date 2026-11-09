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

// correlationFixtures builds, by name, each document a test below derives keys
// over, so TestCorrelationVersion_PinsTheDerivation derives every key of each.
var correlationFixtures = map[string]func() *ir.Document{
	"routes":     routesFixture,
	"params":     paramsFixture,
	"positions":  positionsFixture,
	"parts":      partsFixture,
	"layer":      layerFixture,
	"callbacks":  callbacksFixture,
	"types":      typesFixture,
	"enum":       enumFixture,
	"conjuncts":  conjunctsFixture,
	"variants":   variantsFixture,
	"shared":     sharedFixture,
	"leastFirst": func() *ir.Document { return leastFixture(false) },
}

// anonAny is an anonymous type holding nothing, keyed only by a position.
func anonAny(id ir.TypeID) *ir.Any {
	return &ir.Any{ID: id, Anonymous: true}
}

// withAnonymous returns defs followed by an anonymous type at each of ids.
func withAnonymous(defs []ir.TypeDef, ids ...string) []ir.TypeDef {
	out := make([]ir.TypeDef, 0, len(defs)+len(ids))
	out = append(out, defs...)
	for _, id := range ids {
		out = append(out, anonAny(ir.TypeID(id)))
	}
	return out
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

// assertKeys asserts the key doc derives for each ID want names.
func assertKeys(t *testing.T, doc *ir.Document, want map[string]string) {
	t.Helper()
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	assert.Empty(t, cmp.Diff(want, keysOf(irfacts.New(doc), ids...)))
}

// routesFixture holds an operation of each kind the route and name rules
// tell apart: two of them share a route once its variables are erased.
func routesFixture() *ir.Document {
	hook := ir.Operation{ID: "op/test/hook", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{
		{Method: "POST", Inbound: ir.HTTPInboundWebhook, WebhookName: "created"},
	}}}
	return opsDoc([]ir.Operation{
		namedOp("op/test/a", "getA", "/a"), namedOp("op/test/b", "", "/b/{id}/c{?q}"), hook,
		namedOp("op/test/open", "", "/x/{y"), {ID: "op/test/bare"}, {ID: "op/test/rpc", Name: ir.Naming{Source: "list"}},
		namedOp("op/test/s1", "first", "/s/{a}"), namedOp("op/test/s2", "", "/s/{b}"),
	})
}

func TestCorrelation_KeysAnOperationByRouteElseName(t *testing.T) {
	t.Parallel()
	assertKeys(t, routesFixture(), map[string]string{
		"op/test/a":    "route:GET ~1a",
		"op/test/b":    "route:GET ~1b~1{}~1c{}",
		"op/test/hook": "webhook:POST created",
		"op/test/open": "route:GET ~1x~1{y",
		"op/test/bare": "",
		"op/test/rpc":  "op:list",
		"op/test/s1":   "op:first",
		"op/test/s2":   "",
	})
}

// paramsFixture holds a parameter of each place one travels.
func paramsFixture() *ir.Document {
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
	return opsDoc([]ir.Operation{op}, anonAny("t/anon/q"))
}

func TestCorrelation_KeysAParameterByWhereItTravels(t *testing.T) {
	t.Parallel()
	assertKeys(t, paramsFixture(), map[string]string{
		"param/test/q":    "route:GET ~1a~1{}/param:query q",
		"param/test/h":    "route:GET ~1a~1{}/param:header x-trace",
		"param/test/f":    "route:GET ~1a~1{}/param:path Fallback",
		"param/test/host": "route:GET ~1a~1{}/arg:region",
		"param/test/none": "",
		"t/anon/q":        "route:GET ~1a~1{}/param:query q/schema",
	})
}

// positionsFixture holds an anonymous type at each position an operation
// names one.
func positionsFixture() *ir.Document {
	op := namedOp("op/test/a", "getA", "/a")
	op.Request = &ir.Payload{Contents: []ir.Content{{
		MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/req"},
		Item: &ir.TypeRef{Target: "t/anon/item"}, File: &ir.FileInfo{Contents: &ir.TypeRef{Target: "t/anon/file"}},
		ItemEncoding: &ir.PartEncoding{Headers: []ir.Header{{ID: "p/test/ih", Name: ir.Naming{Source: "X-I"}}}},
	}, {MediaType: "text/plain", Type: ir.TypeRef{Target: "t/anon/text"}}}}
	op.Responses = []ir.Response{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}, {From: 400, To: 499}}},
		Payload:    &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/ok"}}}},
		Headers: []ir.Header{{
			ID: "p/test/rh", Name: ir.Naming{Source: "Retry-After"}, Type: ir.TypeRef{Target: "t/anon/rh"},
			Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t/anon/rhd"}},
		}},
	}}
	op.Default = &ir.Response{
		Payload: &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/def"}}}},
		Headers: []ir.Header{{ID: "p/test/dh", WireName: "X-D"}},
	}
	op.Errors = []ir.ErrorCase{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 500, To: 599}}},
		Payload:    &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/err"}}}},
	}}
	op.RequestStream = &ir.StreamDetail{Events: &ir.TypeRef{Target: "t/anon/in"}}
	op.ResponseStream = &ir.StreamDetail{Initial: &ir.TypeRef{Target: "t/anon/init"}}
	op.Bindings.RPC = &ir.RPCBinding{InputType: &ir.TypeRef{Target: "t/anon/rpc"}}
	op.LongRunning = &ir.LongRunning{PollingType: &ir.TypeRef{Target: "t/anon/poll"}, FinalType: &ir.TypeRef{Target: "t/anon/final"}}
	defs := withAnonymous(nil, "t/anon/req", "t/anon/item", "t/anon/file", "t/anon/text", "t/anon/ok", "t/anon/rh",
		"t/anon/rhd", "t/anon/def", "t/anon/err", "t/anon/in", "t/anon/init", "t/anon/rpc", "t/anon/poll", "t/anon/final")
	return opsDoc([]ir.Operation{op}, defs...)
}

func TestCorrelation_KeysWhatAnOperationNamesByItsPosition(t *testing.T) {
	t.Parallel()
	assertKeys(t, positionsFixture(), map[string]string{
		"t/anon/req":   "route:GET ~1a/request/content:application~1json",
		"t/anon/item":  "route:GET ~1a/request/content:application~1json/item",
		"t/anon/file":  "route:GET ~1a/request/content:application~1json/file",
		"p/test/ih":    "route:GET ~1a/request/content:application~1json/item/header:x-i",
		"t/anon/text":  "route:GET ~1a/request/content:text~1plain",
		"t/anon/ok":    "route:GET ~1a/response:200,400-499/content:application~1json",
		"p/test/rh":    "route:GET ~1a/response:200,400-499/header:retry-after",
		"t/anon/rh":    "route:GET ~1a/response:200,400-499/header:retry-after/type",
		"t/anon/rhd":   "route:GET ~1a/response:200,400-499/header:retry-after/decoded",
		"t/anon/def":   "route:GET ~1a/default/content:application~1json",
		"p/test/dh":    "route:GET ~1a/default/header:x-d",
		"t/anon/err":   "route:GET ~1a/error:500-599/content:application~1json",
		"t/anon/in":    "route:GET ~1a/requestStream/events",
		"t/anon/init":  "route:GET ~1a/responseStream/initial",
		"t/anon/rpc":   "route:GET ~1a/input",
		"t/anon/poll":  "route:GET ~1a/polling",
		"t/anon/final": "route:GET ~1a/final",
	})
}

// partsFixture holds a multipart body at each payload of an operation and a
// service, each encoding one property of its own whose part declares a header,
// and two parts of one property declaring a header each.
func partsFixture() *ir.Document {
	holder := func(name string) *ir.Model {
		return irtest.ModelOf(ir.TypeID("t/test/"+name), func(m *ir.Model) {
			m.Name = ir.Naming{Source: name}
			m.Properties = []ir.Property{{ID: ir.PropID("p/test/" + name), Name: ir.Naming{Source: "part"}}}
		})
	}
	multipart := func(name string, header ir.PropID) *ir.Payload {
		return &ir.Payload{Contents: []ir.Content{{
			MediaType: "multipart/form-data", Type: ir.TypeRef{Target: ir.TypeID("t/test/" + name)},
			Encoding: map[ir.PropID]ir.PartEncoding{ir.PropID("p/test/" + name): {Headers: []ir.Header{{
				ID: header, Name: ir.Naming{Source: "X-Part"}, Type: ir.TypeRef{Target: ir.TypeID("t/anon/" + header)},
			}}}},
		}}}
	}
	op := namedOp("op/test/a", "", "/a")
	op.Request = multipart("Req", "req")
	op.Responses = []ir.Response{{Payload: multipart("Res", "res")}}
	op.Default = &ir.Response{Payload: multipart("Def", "def")}
	op.Errors = []ir.ErrorCase{{Payload: multipart("Err", "err")}, {Payload: multipart("Twice", "twice1")}}
	doc := opsDoc([]ir.Operation{op}, holder("Req"), holder("Res"), holder("Def"), holder("Err"), holder("Common"),
		holder("Twice"), anonAny("t/anon/req"))
	doc.Services[0].CommonErrors = []ir.ErrorCase{{Payload: multipart("Common", "common")}, {Payload: multipart("Twice", "twice2")}}
	return doc
}

func TestCorrelation_KeysAPartsHeadersBeneathThePropertyItEncodes(t *testing.T) {
	t.Parallel()
	assertKeys(t, partsFixture(), map[string]string{
		"req": "type:Req/prop:part/header:x-part", "t/anon/req": "type:Req/prop:part/header:x-part/type",
		"res": "type:Res/prop:part/header:x-part", "def": "type:Def/prop:part/header:x-part",
		"err": "type:Err/prop:part/header:x-part", "common": "type:Common/prop:part/header:x-part",
		"twice1": "", "twice2": "",
	})
}

func TestCorrelation_WalksGroupsThatShareASliceAsTheIRWalkDoes(t *testing.T) {
	t.Parallel()
	looped := []ir.OperationGroup{{ID: "g/test/a"}, {ID: "g/test/b"}}
	looped[0].Groups, looped[1].Groups = looped, looped
	shared := []ir.OperationGroup{{ID: "g/test/c", Operations: []ir.Operation{namedOp("op/test/c", "", "/c")}}}
	doc := irtest.DocOf()
	doc.Services = []ir.Service{{Groups: looped}, {Groups: []ir.OperationGroup{{Groups: shared}, {Groups: shared}}}}
	assertKeys(t, doc, map[string]string{
		"g/test/a": "group:g~1test~1a", "g/test/b": "group:g~1test~1b", "g/test/c": "group:g~1test~1c",
		"op/test/c": "route:GET ~1c",
	})
}

// layerFixture holds a named and an unnamed service, servers a service and
// two operations name, and a named and an unnamed auth scheme.
func layerFixture() *ir.Document {
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
	return doc
}

func TestCorrelation_KeysTheServiceLayerByName(t *testing.T) {
	t.Parallel()
	assertKeys(t, layerFixture(), map[string]string{
		"s/test/0":         "service:Svc",
		"s/test/1":         "",
		"t/anon/common":    "service:Svc/error:/content:application~1json",
		"server/test/root": "service:Svc/server:https:~1~1api.example.com~1v1",
		"server/test/path": "route:GET ~1a/server:https:~1~1{region}.example.com",
		"auth/test/key":    "auth:api_key",
		"auth/test/anon":   "",
		"t/anon/cred":      "auth:api_key/credentials",
		"g/default":        "group:g~1default",
	})
}

func TestCorrelation_KeysAGroupOnlyWithinTheDepthBound(t *testing.T) {
	t.Parallel()
	ids := make([]string, ir.MaxGroupDepth+2)
	nested := make([]ir.OperationGroup, 0, 1)
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

// callbacksFixture holds an operation declaring callbacks: one unnamed, one
// named, one with no binding, one the document does not declare, and one to a
// constant URL spelling the operation's own route.
func callbacksFixture() *ir.Document {
	parent := namedOp("op/test/sub", "subscribe", "/sub")
	parent.Bindings.HTTP[0].Callbacks = []ir.Callback{{
		Name: "on/event", Expression: "{$request.body#/url}",
		Operations: []ir.OpID{"op/test/cb", "op/test/named", "op/test/nobinding", "op/test/missing"},
	}}
	cb := ir.Operation{ID: "op/test/cb", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "POST"}}}}
	// A callback's own route and name sort before the key through its parent,
	// so a callback seeded with either would settle on it.
	named := namedOp("op/test/named", "onEvent", "{$request.body#/url}")
	// A callback to a constant URL spells its parent's route, which the parent
	// shares with no other operation.
	parent.Bindings.HTTP[0].Callbacks = append(parent.Bindings.HTTP[0].Callbacks,
		ir.Callback{Name: "echo", Expression: "/sub", Operations: []ir.OpID{"op/test/same"}})
	same := namedOp("op/test/same", "", "/sub")
	return opsDoc([]ir.Operation{parent, cb, named, {ID: "op/test/nobinding"}, same})
}

func TestCorrelation_KeysACallbackThroughTheOperationDeclaringIt(t *testing.T) {
	t.Parallel()
	assertKeys(t, callbacksFixture(), map[string]string{
		"op/test/sub":       "route:GET ~1sub",
		"op/test/cb":        "route:GET ~1sub/callback:on~1event/method:POST",
		"op/test/named":     "route:GET ~1sub/callback:on~1event/method:GET",
		"op/test/nobinding": "",
		"op/test/same":      "route:GET ~1sub/callback:echo/method:GET",
	})
}

// typesFixture holds a namespaced model whose members reach an anonymous type
// at each position a type names one, and a primitive.
func typesFixture() *ir.Document {
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
	list := &ir.List{ID: "t/anon/list", Anonymous: true, Elem: ir.TypeRef{Target: "t/anon/map"},
		Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t/anon/ld"}}}
	mapT := &ir.MapT{ID: "t/anon/map", Anonymous: true,
		Key: &ir.TypeRef{Target: "t/anon/mk"}, Value: ir.TypeRef{Target: "t/anon/tuple"}}
	tuple := &ir.Tuple{ID: "t/anon/tuple", Anonymous: true,
		Elems: []ir.TypeRef{{Target: "t/anon/e0"}, {Target: "t/anon/scalar"}}, Rest: &ir.TypeRef{Target: "t/anon/rest"}}
	scalar := &ir.Scalar{ID: "t/anon/scalar", Anonymous: true,
		Base: &ir.TypeRef{Target: "t/anon/base"}, Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t/anon/sd"}}}
	defs := withAnonymous([]ir.TypeDef{holder, list, mapT, tuple, scalar, &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}},
		"t/anon/a", "t/anon/ad", "t/anon/arg", "t/anon/av", "t/anon/ak", "t/anon/pv", "t/anon/ld",
		"t/anon/mk", "t/anon/e0", "t/anon/rest", "t/anon/base", "t/anon/sd")
	var typedNil *ir.List
	doc := irtest.DocOf(defs...)
	doc.Types["t/test/nil"] = typedNil
	return doc
}

func TestCorrelation_KeysWhatATypeHolds(t *testing.T) {
	t.Parallel()
	h := "ns:acme/ns:v1/type:Holder"
	assertKeys(t, typesFixture(), map[string]string{
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
	})
}

// enumFixture holds an enum with a member beneath it, a repeat of that
// member's value and a member outside its namespace, and an enum whose ID has
// no kind of its own.
func enumFixture() *ir.Document {
	return irtest.DocOf(&ir.Enum{ID: "t/test/Color", Name: ir.Naming{Source: "Color"},
		Members: []ir.EnumMember{{ID: "e/test/Color/s:red"}, {ID: "e/test/Color/s:red/2"}, {ID: "e/other/Color/s:blue"}}},
		&ir.Enum{ID: "Shade", Name: ir.Naming{Source: "Shade"}, Members: []ir.EnumMember{{ID: "e/Shade/s:dark"}}})
}

func TestCorrelation_KeysAnEnumMemberBeneathItsEnum(t *testing.T) {
	t.Parallel()
	assertKeys(t, enumFixture(), map[string]string{
		"e/test/Color/s:red": "type:Color/member:s:red", "e/test/Color/s:red/2": "type:Color/member:s:red~12",
		"e/other/Color/s:blue": "", "e/Shade/s:dark": "",
	})
}

// conjunctsFixture holds a model composing anonymous models in place, one of
// them through another, beside a named base, and one composing a single one.
func conjunctsFixture() *ir.Document {
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
	return doc
}

func TestCorrelation_KeysAModelsInPlaceConjunctsAsItsOwn(t *testing.T) {
	t.Parallel()
	assertKeys(t, conjunctsFixture(), map[string]string{
		"p/test/n": "type:N/prop:n", "p/test/x": "type:M/prop:x", "p/test/y1": "", "p/test/y2": "type:M/prop:y",
		"p/test/z": "type:M/prop:z", "t/anon/c1": "", "t/anon/c2": "", "t/anon/c3": "",
		"t/anon/extra": "type:M/additional", "t/anon/solo": "type:S/allOf", "p/test/q": "type:S/prop:q",
	})
}

// variantsFixture holds a union a discriminator tags, one keyed by its
// variants' names on the wire, and an untagged one.
func variantsFixture() *ir.Document {
	mapped := &ir.Union{ID: "t/test/Mapped", Name: ir.Naming{Source: "Mapped"},
		WireTagged: true,
		Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{
			"b": "t/anon/b", "a2": "t/anon/a", "a": "t/anon/a",
		}},
		Variants: []ir.Variant{
			{Type: ir.TypeRef{Target: "t/anon/a"}}, {Type: ir.TypeRef{Target: "t/anon/b"}},
			{Name: ir.Naming{Source: "c"}, Type: ir.TypeRef{Target: "t/anon/c"}}, {Type: ir.TypeRef{Target: "t/test/Named"}},
		}}
	keyed := &ir.Union{ID: "t/test/Keyed", Name: ir.Naming{Source: "Keyed"}, WireTagged: true,
		Variants: []ir.Variant{
			{WireName: "w", Type: ir.TypeRef{Target: "t/anon/w"}}, {Name: ir.Naming{Source: "n"}, Type: ir.TypeRef{Target: "t/anon/n"}},
			{Type: ir.TypeRef{Target: "t/anon/none"}},
		}}
	untagged := &ir.Union{ID: "t/test/Plain", Name: ir.Naming{Source: "Plain"},
		Variants: []ir.Variant{
			{Name: ir.Naming{Source: "v"}, Type: ir.TypeRef{Target: "t/anon/v"}}, {Type: ir.TypeRef{Target: "t/anon/bare"}},
		}}
	named := irtest.ModelOf("t/test/Named", func(m *ir.Model) { m.Name = ir.Naming{Source: "Named"} })
	return irtest.DocOf(withAnonymous([]ir.TypeDef{mapped, keyed, untagged, named},
		"t/anon/a", "t/anon/b", "t/anon/c", "t/anon/w", "t/anon/n", "t/anon/none", "t/anon/v", "t/anon/bare")...)
}

func TestCorrelation_KeysAVariantsTypeByWhatNamesTheVariant(t *testing.T) {
	t.Parallel()
	assertKeys(t, variantsFixture(), map[string]string{
		"t/anon/a": "type:Mapped/tag:a", "t/anon/b": "type:Mapped/tag:b", "t/anon/c": "type:Mapped/variant:c",
		"t/anon/w": "type:Keyed/tag:w", "t/anon/n": "type:Keyed/tag:n", "t/anon/none": "",
		"t/anon/v": "type:Plain/variant:v", "t/anon/bare": "", "t/test/Named": "type:Named",
	})
}

// sharedFixture holds two models of one name in two namespaces of IDs, and a
// model with no name.
func sharedFixture() *ir.Document {
	pet := func(id ir.TypeID, prop ir.PropID, held ir.TypeID) *ir.Model {
		return irtest.ModelOf(id, func(m *ir.Model) {
			m.Name = ir.Naming{Source: "Pet"}
			m.Properties = []ir.Property{{ID: prop, Name: ir.Naming{Source: string(prop)}, Type: ir.TypeRef{Target: held}}}
		})
	}
	return irtest.DocOf(pet("t/one/Pet", "p/one/id", "t/anon/one"), pet("t/two/Pet", "p/two/tag", "t/anon/two"),
		anonAny("t/anon/one"), anonAny("t/anon/two"), irtest.ModelOf("t/test/Anon", func(*ir.Model) {}))
}

func TestCorrelation_ASharedKeyKeysNeitherEntityNorWhatTheyName(t *testing.T) {
	t.Parallel()
	facts := irfacts.New(sharedFixture())
	got := keysOf(facts, "t/one/Pet", "t/two/Pet", "p/one/id", "p/two/tag", "t/anon/one", "t/test/Anon")
	for id, key := range got {
		assert.Empty(t, key, id)
	}
	_, found := facts.Correlated("type:Pet")
	assert.False(t, found, "a key two entities share names neither")
}

// leastFixture holds two operations naming one anonymous type each, declared
// in either order, and a map naming one type as its key and its value.
func leastFixture(swapped bool) *ir.Document {
	withParam := func(id ir.OpID, name string) ir.Operation {
		op := namedOp(id, name, "/"+name)
		op.Params = []ir.Parameter{{ID: ir.ParamID("param/test/" + name), Name: ir.Naming{Source: "p"}, Type: ir.TypeRef{Target: "t/anon/shared"}}}
		op.Request = &ir.Payload{Contents: []ir.Content{
			{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/twice"}},
			{MediaType: "application/json", Type: ir.TypeRef{Target: "t/anon/twice"}},
		}}
		return op
	}
	pair := &ir.MapT{ID: "t/test/Pair", Name: ir.Naming{Source: "Pair"},
		Key: &ir.TypeRef{Target: "t/anon/kv"}, Value: ir.TypeRef{Target: "t/anon/kv"}}
	ops := []ir.Operation{withParam("op/test/b", "b"), withParam("op/test/a", "a")}
	if swapped {
		slices.Reverse(ops)
	}
	return opsDoc(ops, anonAny("t/anon/shared"), anonAny("t/anon/twice"), anonAny("t/anon/kv"), pair)
}

func TestCorrelation_TakesTheLeastKeyAmongThePositionsNamingAType(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		doc := leastFixture(swapped)
		assertKeys(t, doc, map[string]string{
			"t/anon/shared": "route:GET ~1a/arg:p/schema",
			"t/anon/twice":  "route:GET ~1a/request/content:application~1json",
			"t/anon/kv":     "type:Pair/key",
		})
		id, ok := irfacts.New(doc).Correlated("route:GET ~1a/arg:p/schema")
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
	assert.False(t, fromZero)
	assert.False(t, fromNil)
	assertKeys(t, doc, map[string]string{"t/test/M": "", "p/test/a": "", "t/test/nil": ""})
}
