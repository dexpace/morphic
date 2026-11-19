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
	"prefixes":   prefixesFixture,
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
func namedOp(id ir.OperationID, name, path string) ir.Operation {
	return ir.Operation{
		ID: id, Name: ir.Naming{Source: name},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "GET", URITemplate: path}}},
	}
}

// opsDoc is a document of types whose one service groups ops.
func opsDoc(ops []ir.Operation, defs ...ir.TypeDef) *ir.Document {
	doc := irtest.DocOf(defs...)
	doc.Services = []ir.Service{{ID: "service/test/0", Groups: []ir.OperationGroup{{ID: "operation-group/default", Operations: ops}}}}
	return doc
}

// keysOf returns the key of each id, "" for one that has none.
func keysOf(facts *irfacts.DocumentFacts, ids ...string) map[string]string {
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
	hook := ir.Operation{ID: "operation/test/hook", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{
		{Method: "POST", Inbound: ir.HTTPInboundWebhook, WebhookName: "created"},
	}}}
	return opsDoc([]ir.Operation{
		namedOp("operation/test/a", "getA", "/a"), namedOp("operation/test/b", "", "/b/{id}/c{?q}"), hook,
		namedOp("operation/test/open", "", "/x/{y"), {ID: "operation/test/bare"}, {ID: "operation/test/rpc", Name: ir.Naming{Source: "list"}},
		namedOp("operation/test/s1", "first", "/s/{a}"), namedOp("operation/test/s2", "", "/s/{b}"),
	})
}

func TestCorrelation_KeysAnOperationByRouteElseName(t *testing.T) {
	t.Parallel()
	assertKeys(t, routesFixture(), map[string]string{
		"operation/test/a":    "route:GET ~1a",
		"operation/test/b":    "route:GET ~1b~1{}~1c{}",
		"operation/test/hook": "webhook:POST created",
		"operation/test/open": "route:GET ~1x~1{y",
		"operation/test/bare": "",
		"operation/test/rpc":  "op:list",
		"operation/test/s1":   "op:first",
		"operation/test/s2":   "",
	})
}

// paramsFixture holds a parameter of each place one travels.
func paramsFixture() *ir.Document {
	op := namedOp("operation/test/a", "getA", "/a/{Fallback}")
	op.Params = []ir.Parameter{
		{ID: "parameter/test/q", Name: ir.Naming{Source: "q"}, Type: ir.TypeRef{Target: "type/anonymous/q"}},
		{ID: "parameter/test/h", Name: ir.Naming{Source: "X-Trace"}},
		{ID: "parameter/test/f", Name: ir.Naming{Source: "Fallback"}},
		{ID: "parameter/test/host", Name: ir.Naming{Source: "region"}},
		{ID: "parameter/test/none"},
	}
	op.Bindings.HTTP[0].ParamBindings = []ir.HTTPParamBinding{
		{Param: "parameter/test/q", Location: ir.HTTPLocationQuery, WireName: "q"},
		{Param: "parameter/test/h", Location: ir.HTTPLocationHeader, WireName: "X-Trace"},
		{Param: "parameter/test/f", Location: ir.HTTPLocationPath},
		{Param: "parameter/test/host", Location: ir.HTTPLocationHost, WireName: "region"},
	}
	return opsDoc([]ir.Operation{op}, anonAny("type/anonymous/q"))
}

func TestCorrelation_KeysAParameterByWhereItTravels(t *testing.T) {
	t.Parallel()
	assertKeys(t, paramsFixture(), map[string]string{
		"parameter/test/q":    "route:GET ~1a~1{}/param:query q",
		"parameter/test/h":    "route:GET ~1a~1{}/param:header x-trace",
		"parameter/test/f":    "route:GET ~1a~1{}/param:path Fallback",
		"parameter/test/host": "route:GET ~1a~1{}/arg:region",
		"parameter/test/none": "",
		"type/anonymous/q":    "route:GET ~1a~1{}/param:query q/schema",
	})
}

// positionsFixture holds an anonymous type at each position an operation
// names one.
func positionsFixture() *ir.Document {
	op := namedOp("operation/test/a", "getA", "/a")
	op.Request = &ir.Payload{Contents: []ir.Content{{
		MediaType: "application/json", Type: ir.TypeRef{Target: "type/anonymous/req"},
		Item: &ir.TypeRef{Target: "type/anonymous/item"}, File: &ir.FileInfo{Contents: &ir.TypeRef{Target: "type/anonymous/file"}},
		ItemEncoding: &ir.PartEncoding{Headers: []ir.Header{{ID: "property/test/ih", Name: ir.Naming{Source: "X-I"}}}},
	}, {MediaType: "text/plain", Type: ir.TypeRef{Target: "type/anonymous/text"}}}}
	op.Responses = []ir.Response{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}, {From: 400, To: 499}}},
		Payload:    &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "type/anonymous/ok"}}}},
		Headers: []ir.Header{{
			ID: "property/test/rh", Name: ir.Naming{Source: "Retry-After"}, Type: ir.TypeRef{Target: "type/anonymous/rh"},
			Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "type/anonymous/rhd"}},
		}},
	}}
	op.Default = &ir.Response{
		Payload: &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "type/anonymous/def"}}}},
		Headers: []ir.Header{{ID: "property/test/dh", WireName: "X-D"}},
	}
	op.Errors = []ir.ErrorCase{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 500, To: 599}}},
		Payload:    &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "type/anonymous/err"}}}},
	}}
	op.RequestStream = &ir.StreamDetail{Events: &ir.TypeRef{Target: "type/anonymous/in"}}
	op.ResponseStream = &ir.StreamDetail{Initial: &ir.TypeRef{Target: "type/anonymous/init"}}
	op.Bindings.RPC = &ir.RPCBinding{InputType: &ir.TypeRef{Target: "type/anonymous/rpc"}}
	op.LongRunning = &ir.LongRunning{PollingType: &ir.TypeRef{Target: "type/anonymous/poll"}, FinalType: &ir.TypeRef{Target: "type/anonymous/final"}}
	defs := withAnonymous(nil, "type/anonymous/req", "type/anonymous/item", "type/anonymous/file", "type/anonymous/text", "type/anonymous/ok", "type/anonymous/rh",
		"type/anonymous/rhd", "type/anonymous/def", "type/anonymous/err", "type/anonymous/in", "type/anonymous/init", "type/anonymous/rpc", "type/anonymous/poll", "type/anonymous/final")
	return opsDoc([]ir.Operation{op}, defs...)
}

func TestCorrelation_KeysWhatAnOperationNamesByItsPosition(t *testing.T) {
	t.Parallel()
	assertKeys(t, positionsFixture(), map[string]string{
		"type/anonymous/req":   "route:GET ~1a/request/content:application~1json",
		"type/anonymous/item":  "route:GET ~1a/request/content:application~1json/item",
		"type/anonymous/file":  "route:GET ~1a/request/content:application~1json/file",
		"property/test/ih":     "route:GET ~1a/request/content:application~1json/item/header:x-i",
		"type/anonymous/text":  "route:GET ~1a/request/content:text~1plain",
		"type/anonymous/ok":    "route:GET ~1a/response:200,400-499/content:application~1json",
		"property/test/rh":     "route:GET ~1a/response:200,400-499/header:retry-after",
		"type/anonymous/rh":    "route:GET ~1a/response:200,400-499/header:retry-after/type",
		"type/anonymous/rhd":   "route:GET ~1a/response:200,400-499/header:retry-after/decoded",
		"type/anonymous/def":   "route:GET ~1a/default/content:application~1json",
		"property/test/dh":     "route:GET ~1a/default/header:x-d",
		"type/anonymous/err":   "route:GET ~1a/error:500-599/content:application~1json",
		"type/anonymous/in":    "route:GET ~1a/requestStream/events",
		"type/anonymous/init":  "route:GET ~1a/responseStream/initial",
		"type/anonymous/rpc":   "route:GET ~1a/input",
		"type/anonymous/poll":  "route:GET ~1a/polling",
		"type/anonymous/final": "route:GET ~1a/final",
	})
}

// prefixesFixture holds a header under a prefix beside a header named as the
// prefix is, and a header under the empty prefix, and the same of a request's
// parameters, with query maps under the empty prefix and under a mixed-case one.
func prefixesFixture() *ir.Document {
	op := namedOp("operation/test/a", "getA", "/a")
	op.Params = []ir.Parameter{
		{ID: "parameter/test/named", Name: ir.Naming{Source: "named"}},
		{ID: "parameter/test/meta", Name: ir.Naming{Source: "meta"}, Type: ir.TypeRef{Target: "type/anonymous/pmeta"}},
		{ID: "parameter/test/all", Name: ir.Naming{Source: "all"}},
		{ID: "parameter/test/rest", Name: ir.Naming{Source: "rest"}},
		{ID: "parameter/test/filter", Name: ir.Naming{Source: "filter"}},
	}
	op.Bindings.HTTP[0].ParamBindings = []ir.HTTPParamBinding{
		{Param: "parameter/test/named", Location: ir.HTTPLocationHeader, WireName: "X-Meta-"},
		{Param: "parameter/test/meta", Location: ir.HTTPLocationHeader, Prefix: new("X-Meta-")},
		{Param: "parameter/test/all", Location: ir.HTTPLocationHeader, Prefix: new("")},
		{Param: "parameter/test/rest", Location: ir.HTTPLocationQuery, Prefix: new("")},
		{Param: "parameter/test/filter", Location: ir.HTTPLocationQuery, Prefix: new("Filter-")},
	}
	op.Responses = []ir.Response{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}}},
		Headers: []ir.Header{
			{ID: "property/test/named", WireName: "X-Meta-"},
			{ID: "property/test/meta", Name: ir.Naming{Source: "meta"}, Type: ir.TypeRef{Target: "type/anonymous/meta"}, Prefix: new("X-Meta-")},
		},
	}}
	op.Errors = []ir.ErrorCase{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 500, To: 500}}},
		Headers:    []ir.Header{{ID: "property/test/all", Name: ir.Naming{Source: "all"}, Prefix: new("")}},
	}}
	return opsDoc([]ir.Operation{op}, withAnonymous(nil, "type/anonymous/meta", "type/anonymous/pmeta")...)
}

// TestCorrelation_KeysAHeaderUnderAPrefixByItsPrefix holds a header under a
// prefix to its prefix rather than its name, apart from a header named as the
// prefix is, and keys the empty prefix too (docs/ir/responses.md#prefix-headers).
func TestCorrelation_KeysAHeaderUnderAPrefixByItsPrefix(t *testing.T) {
	t.Parallel()
	assertKeys(t, prefixesFixture(), map[string]string{
		"property/test/named": "route:GET ~1a/response:200/header:x-meta-",
		"property/test/meta":  "route:GET ~1a/response:200/prefix:x-meta-",
		"type/anonymous/meta": "route:GET ~1a/response:200/prefix:x-meta-/type",
		"property/test/all":   "route:GET ~1a/error:500/prefix:",
	})
}

// TestCorrelation_KeysAParameterUnderAPrefixByItsPrefix holds a parameter
// bound under a prefix to where it travels and its prefix rather than its
// name, apart from a parameter whose wire name is the prefix, keys the empty
// prefix of a header and of a query apart, and keeps a query prefix's case
// (docs/ir/http-binding.md#prefix-bindings).
func TestCorrelation_KeysAParameterUnderAPrefixByItsPrefix(t *testing.T) {
	t.Parallel()
	assertKeys(t, prefixesFixture(), map[string]string{
		"parameter/test/named":  "route:GET ~1a/param:header x-meta-",
		"parameter/test/meta":   "route:GET ~1a/header/prefix:x-meta-",
		"type/anonymous/pmeta":  "route:GET ~1a/header/prefix:x-meta-/schema",
		"parameter/test/all":    "route:GET ~1a/header/prefix:",
		"parameter/test/rest":   "route:GET ~1a/query/prefix:",
		"parameter/test/filter": "route:GET ~1a/query/prefix:Filter-",
	})
}

// partsFixture holds a multipart body at each payload of an operation and a
// service, each encoding one property of its own whose part declares a header,
// and two parts of one property declaring a header each.
func partsFixture() *ir.Document {
	holder := func(name string) *ir.Model {
		return irtest.ModelOf(ir.TypeID("type/test/"+name), func(m *ir.Model) {
			m.Name = ir.Naming{Source: name}
			m.Properties = []ir.Property{{ID: ir.PropertyID("property/test/" + name), Name: ir.Naming{Source: "part"}}}
		})
	}
	multipart := func(name string, header ir.PropertyID) *ir.Payload {
		return &ir.Payload{Contents: []ir.Content{{
			MediaType: "multipart/form-data", Type: ir.TypeRef{Target: ir.TypeID("type/test/" + name)},
			Encoding: map[ir.PropertyID]ir.PartEncoding{ir.PropertyID("property/test/" + name): {Headers: []ir.Header{{
				ID: header, Name: ir.Naming{Source: "X-Part"}, Type: ir.TypeRef{Target: ir.TypeID("type/anonymous/" + header)},
			}}}},
		}}}
	}
	op := namedOp("operation/test/a", "", "/a")
	op.Request = multipart("Req", "req")
	op.Responses = []ir.Response{{Payload: multipart("Res", "res")}}
	op.Default = &ir.Response{Payload: multipart("Def", "def")}
	op.Errors = []ir.ErrorCase{{Payload: multipart("Err", "err")}, {Payload: multipart("Twice", "twice1")}}
	doc := opsDoc([]ir.Operation{op}, holder("Req"), holder("Res"), holder("Def"), holder("Err"), holder("Common"),
		holder("Twice"), anonAny("type/anonymous/req"))
	doc.Services[0].CommonErrors = []ir.ErrorCase{{Payload: multipart("Common", "common")}, {Payload: multipart("Twice", "twice2")}}
	return doc
}

// TestCorrelation_KeysAServerALinkNames pins that a server only a link names is
// keyed by the response carrying the link and the link's name, on a success
// response, an error case and a default alike.
func TestCorrelation_KeysAServerALinkNames(t *testing.T) {
	t.Parallel()
	link := func(name string, server ir.ServerID) []ir.OperationLink {
		return []ir.OperationLink{{Name: ir.Naming{Source: name}, Operation: "operation/test/a", Server: &server}}
	}
	op := namedOp("operation/test/a", "getA", "/a")
	op.Responses = []ir.Response{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 201, To: 201}}},
		Links:      link("Next", "server/test/ok"),
	}}
	op.Errors = []ir.ErrorCase{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 409, To: 409}}},
		Links:      link("Next", "server/test/err"),
	}}
	op.Default = &ir.Response{Links: link("Retry", "server/test/def")}
	doc := opsDoc([]ir.Operation{op})
	doc.Servers = map[ir.ServerID]ir.Server{}
	for _, id := range []ir.ServerID{"server/test/ok", "server/test/err", "server/test/def"} {
		doc.Servers[id] = ir.Server{ID: id, URLTemplate: "https://" + string(id[len("server/test/"):]) + ".example"}
	}
	got := keysOf(irfacts.New(doc), "server/test/ok", "server/test/err", "server/test/def")
	assert.Empty(t, cmp.Diff(map[string]string{
		"server/test/ok":  "route:GET ~1a/response:201/link:Next/server:https:~1~1ok.example",
		"server/test/err": "route:GET ~1a/error:409/link:Next/server:https:~1~1err.example",
		"server/test/def": "route:GET ~1a/default/link:Retry/server:https:~1~1def.example",
	}, got))
}

func TestCorrelation_KeysAPartsHeadersBeneathThePropertyItEncodes(t *testing.T) {
	t.Parallel()
	assertKeys(t, partsFixture(), map[string]string{
		"req": "type:Req/prop:part/header:x-part", "type/anonymous/req": "type:Req/prop:part/header:x-part/type",
		"res": "type:Res/prop:part/header:x-part", "def": "type:Def/prop:part/header:x-part",
		"err": "type:Err/prop:part/header:x-part", "common": "type:Common/prop:part/header:x-part",
		"twice1": "", "twice2": "",
	})
}

func TestCorrelation_WalksGroupsThatShareASliceAsTheIRWalkDoes(t *testing.T) {
	t.Parallel()
	looped := []ir.OperationGroup{{ID: "operation-group/test/a"}, {ID: "operation-group/test/b"}}
	looped[0].Groups, looped[1].Groups = looped, looped
	shared := []ir.OperationGroup{{ID: "operation-group/test/c", Operations: []ir.Operation{namedOp("operation/test/c", "", "/c")}}}
	doc := irtest.DocOf()
	doc.Services = []ir.Service{{Groups: looped}, {Groups: []ir.OperationGroup{{Groups: shared}, {Groups: shared}}}}
	assertKeys(t, doc, map[string]string{
		"operation-group/test/a": "group:operation-group~1test~1a", "operation-group/test/b": "group:operation-group~1test~1b", "operation-group/test/c": "group:operation-group~1test~1c",
		"operation/test/c": "route:GET ~1c",
	})
}

// layerFixture holds a named and an unnamed service, servers a service and
// two operations name, and a named and an unnamed auth scheme.
func layerFixture() *ir.Document {
	doc := opsDoc([]ir.Operation{namedOp("operation/test/b", "b", "/b"), namedOp("operation/test/a", "a", "/a")},
		anonAny("type/anonymous/common"), anonAny("type/anonymous/cred"))
	doc.Services[0].Name = ir.Naming{Source: "Svc"}
	doc.Services[0].Servers = []ir.ServerID{"server/test/root", "server/test/missing"}
	doc.Services[0].CommonErrors = []ir.ErrorCase{{Payload: &ir.Payload{Contents: []ir.Content{
		{MediaType: "application/json", Type: ir.TypeRef{Target: "type/anonymous/common"}},
	}}}}
	doc.Services[0].Groups[0].Operations[0].Servers = []ir.ServerID{"server/test/path"}
	doc.Services[0].Groups[0].Operations[1].Servers = []ir.ServerID{"server/test/path"}
	doc.Services = append(doc.Services, ir.Service{ID: "service/test/1"})
	doc.Servers = map[ir.ServerID]ir.Server{
		"server/test/root": {ID: "server/test/root", URLTemplate: "https://api.example.com/v1"},
		"server/test/path": {ID: "server/test/path", URLTemplate: "https://{region}.example.com"},
	}
	doc.Auth = map[ir.AuthID]ir.AuthScheme{
		"auth/test/key":  {ID: "auth/test/key", Name: ir.Naming{Source: "api_key"}, Credentials: &ir.TypeRef{Target: "type/anonymous/cred"}},
		"auth/test/anon": {ID: "auth/test/anon"},
	}
	return doc
}

func TestCorrelation_KeysTheServiceLayerByName(t *testing.T) {
	t.Parallel()
	assertKeys(t, layerFixture(), map[string]string{
		"service/test/0":          "service:Svc",
		"service/test/1":          "",
		"type/anonymous/common":   "service:Svc/error:/content:application~1json",
		"server/test/root":        "service:Svc/server:https:~1~1api.example.com~1v1",
		"server/test/path":        "route:GET ~1a/server:https:~1~1{region}.example.com",
		"auth/test/key":           "auth:api_key",
		"auth/test/anon":          "",
		"type/anonymous/cred":     "auth:api_key/credentials",
		"operation-group/default": "group:operation-group~1default",
	})
}

func TestCorrelation_KeysAGroupOnlyWithinTheDepthBound(t *testing.T) {
	t.Parallel()
	ids := make([]string, ir.MaxGroupDepth+2)
	nested := make([]ir.OperationGroup, 0, 1)
	for depth := len(ids) - 1; depth >= 0; depth-- {
		ids[depth] = "operation-group/test/" + string(rune('a'+depth%26)) + string(rune('a'+depth/26))
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
// named, one with no binding, one the document does not declare, one to a
// constant URL spelling the operation's own route, one under another name to
// that URL, and two of one method that one name keys by two expressions.
func callbacksFixture() *ir.Document {
	parent := namedOp("operation/test/sub", "subscribe", "/sub")
	parent.Bindings.HTTP[0].Callbacks = []ir.Callback{{
		Name: "on/event", Expression: "{$request.body#/url}",
		Operations: []ir.OperationID{"operation/test/cb", "operation/test/named", "operation/test/nobinding", "operation/test/missing"},
	}}
	cb := ir.Operation{ID: "operation/test/cb", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "POST"}}}}
	// A callback's own route and name sort before the key through its parent,
	// so a callback seeded with either would settle on it.
	named := namedOp("operation/test/named", "onEvent", "{$request.body#/url}")
	// A callback to a constant URL spells its parent's route, which the parent
	// shares with no other operation.
	parent.Bindings.HTTP[0].Callbacks = append(parent.Bindings.HTTP[0].Callbacks,
		ir.Callback{Name: "echo", Expression: "/sub", Operations: []ir.OperationID{"operation/test/same"}})
	same := namedOp("operation/test/same", "", "/sub")
	put := func(id ir.OperationID) ir.Operation {
		return ir.Operation{ID: id, Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "PUT"}}}}
	}
	parent.Bindings.HTTP[0].Callbacks = append(parent.Bindings.HTTP[0].Callbacks,
		ir.Callback{Name: "alias", Expression: "/sub", Operations: []ir.OperationID{"operation/test/alias"}},
		ir.Callback{Name: "twice", Expression: "{$request.body#/a}", Operations: []ir.OperationID{"operation/test/twiceA"}},
		ir.Callback{Name: "twice", Expression: "{$request.body#/b}", Operations: []ir.OperationID{"operation/test/twiceB"}})
	alias := namedOp("operation/test/alias", "", "/sub")
	return opsDoc([]ir.Operation{parent, cb, named, {ID: "operation/test/nobinding"}, same, alias,
		put("operation/test/twiceA"), put("operation/test/twiceB")})
}

func TestCorrelation_KeysACallbackThroughTheOperationDeclaringIt(t *testing.T) {
	t.Parallel()
	assertKeys(t, callbacksFixture(), map[string]string{
		"operation/test/sub":       "route:GET ~1sub",
		"operation/test/cb":        "route:GET ~1sub/callback:on~1event/method:POST",
		"operation/test/named":     "route:GET ~1sub/callback:on~1event/method:GET",
		"operation/test/nobinding": "",
		"operation/test/same":      "route:GET ~1sub/callback:echo/method:GET",
		"operation/test/alias":     "route:GET ~1sub/callback:alias/method:GET",
		"operation/test/twiceA":    "route:GET ~1sub/callback:twice/expression:{$request.body#~1a}/method:PUT",
		"operation/test/twiceB":    "route:GET ~1sub/callback:twice/expression:{$request.body#~1b}/method:PUT",
	})
}

// typesFixture holds a namespaced model whose members reach an anonymous type
// at each position a type names one, and a primitive.
func typesFixture() *ir.Document {
	holder := irtest.ModelOf("type/test/Holder", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "Holder"}
		m.Namespace = []string{"acme", "v1"}
		m.Properties = []ir.Property{{
			ID: "property/test/a", Name: ir.Naming{Source: "a"}, WireName: "A", Type: ir.TypeRef{Target: "type/anonymous/a"},
			Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "type/anonymous/ad"}},
			Args:     []ir.Parameter{{ID: "parameter/test/arg", Name: ir.Naming{Source: "first"}, Type: ir.TypeRef{Target: "type/anonymous/arg"}}},
		}, {ID: "property/test/l", Name: ir.Naming{Source: "l"}, Type: ir.TypeRef{Target: "type/anonymous/list"}},
			{ID: "property/test/gone", Name: ir.Naming{Source: "gone"}, Type: ir.TypeRef{Target: "type/missing"}},
			{ID: "property/test/nil", Name: ir.Naming{Source: "nil"}, Type: ir.TypeRef{Target: "type/test/nil"}}}
		m.AdditionalProperties = &ir.AdditionalProperties{
			Value: &ir.TypeRef{Target: "type/anonymous/av"}, Key: &ir.TypeRef{Target: "type/anonymous/ak"},
			Patterns: []ir.PatternProperties{{Pattern: "^x-/", Value: ir.TypeRef{Target: "type/anonymous/pv"}}},
		}
	})
	list := &ir.List{ID: "type/anonymous/list", Anonymous: true, ElementType: ir.TypeRef{Target: "type/anonymous/map"},
		Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "type/anonymous/ld"}}}
	mapT := &ir.MapT{ID: "type/anonymous/map", Anonymous: true,
		Key: &ir.TypeRef{Target: "type/anonymous/mk"}, Value: ir.TypeRef{Target: "type/anonymous/tuple"}}
	tuple := &ir.Tuple{ID: "type/anonymous/tuple", Anonymous: true,
		ElementTypes: []ir.TypeRef{{Target: "type/anonymous/e0"}, {Target: "type/anonymous/scalar"}}, Rest: &ir.TypeRef{Target: "type/anonymous/rest"}}
	scalar := &ir.Scalar{ID: "type/anonymous/scalar", Anonymous: true,
		Base: &ir.TypeRef{Target: "type/anonymous/base"}, Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "type/anonymous/sd"}}}
	defs := withAnonymous([]ir.TypeDef{holder, list, mapT, tuple, scalar, &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString}},
		"type/anonymous/a", "type/anonymous/ad", "type/anonymous/arg", "type/anonymous/av", "type/anonymous/ak", "type/anonymous/pv", "type/anonymous/ld",
		"type/anonymous/mk", "type/anonymous/e0", "type/anonymous/rest", "type/anonymous/base", "type/anonymous/sd")
	var typedNil *ir.List
	doc := irtest.DocOf(defs...)
	doc.Types["type/test/nil"] = typedNil
	return doc
}

func TestCorrelation_KeysWhatATypeHolds(t *testing.T) {
	t.Parallel()
	h := "ns:acme/ns:v1/type:Holder"
	assertKeys(t, typesFixture(), map[string]string{
		"type/test/Holder": h, "type/primitive/string": "prim:string",
		"property/test/a": h + "/prop:A", "type/anonymous/a": h + "/prop:A/type", "type/anonymous/ad": h + "/prop:A/decoded",
		"parameter/test/arg": h + "/prop:A/arg:first", "type/anonymous/arg": h + "/prop:A/arg:first/schema",
		"type/anonymous/av": h + "/additional", "type/anonymous/ak": h + "/additionalKey", "type/anonymous/pv": h + "/pattern:^x-~1",
		"property/test/gone": h + "/prop:gone",
		"property/test/l":    h + "/prop:l", "type/anonymous/list": h + "/prop:l/type", "type/anonymous/ld": h + "/prop:l/type/decoded",
		"type/anonymous/map": h + "/prop:l/type/elem", "type/anonymous/mk": h + "/prop:l/type/elem/key",
		"type/anonymous/tuple": h + "/prop:l/type/elem/value", "type/anonymous/e0": h + "/prop:l/type/elem/value/elem:0",
		"type/anonymous/scalar": h + "/prop:l/type/elem/value/elem:1", "type/anonymous/rest": h + "/prop:l/type/elem/value/rest",
		"type/anonymous/base": h + "/prop:l/type/elem/value/elem:1/base", "type/anonymous/sd": h + "/prop:l/type/elem/value/elem:1/decoded",
	})
}

// enumFixture holds an enum with a member beneath it, a repeat of that
// member's value and a member outside its namespace, and an enum whose ID has
// no kind of its own.
func enumFixture() *ir.Document {
	return irtest.DocOf(&ir.Enum{ID: "type/test/Color", Name: ir.Naming{Source: "Color"},
		Members: []ir.EnumMember{{ID: "enum-member/test/Color/string:red"}, {ID: "enum-member/test/Color/string:red/2"}, {ID: "enum-member/other/Color/string:blue"}}},
		&ir.Enum{ID: "Shade", Name: ir.Naming{Source: "Shade"}, Members: []ir.EnumMember{{ID: "enum-member/Shade/string:dark"}}})
}

func TestCorrelation_KeysAnEnumMemberBeneathItsEnum(t *testing.T) {
	t.Parallel()
	assertKeys(t, enumFixture(), map[string]string{
		"enum-member/test/Color/string:red": "type:Color/member:string:red", "enum-member/test/Color/string:red/2": "type:Color/member:string:red~12",
		"enum-member/other/Color/string:blue": "", "enum-member/Shade/string:dark": "",
	})
}

// conjunctsFixture holds a model composing anonymous models in place, one of
// them through another, beside a named base, and one composing a single one.
func conjunctsFixture() *ir.Document {
	base := irtest.ModelOf("type/test/N", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "N"}
		m.Properties = []ir.Property{{ID: "property/test/n", Name: ir.Naming{Source: "n"}}}
	})
	inner := irtest.ModelOf("type/anonymous/c3", func(m *ir.Model) {
		m.Anonymous = true
		m.Properties = []ir.Property{{ID: "property/test/z", Name: ir.Naming{Source: "z"}}}
		m.Mixins = irtest.RefsTo("type/anonymous/c2")
	})
	c1 := irtest.ModelOf("type/anonymous/c1", func(m *ir.Model) {
		m.Anonymous = true
		m.Properties = []ir.Property{{ID: "property/test/x", Name: ir.Naming{Source: "x"}}, {ID: "property/test/y1", Name: ir.Naming{Source: "y"}}}
	})
	c2 := irtest.ModelOf("type/anonymous/c2", func(m *ir.Model) {
		m.Anonymous = true
		m.Mixins = irtest.RefsTo("type/anonymous/c3", "type/missing", "type/anonymous/c3")
		m.AdditionalProperties = &ir.AdditionalProperties{Value: &ir.TypeRef{Target: "type/anonymous/extra"}}
	})
	solo := irtest.ModelOf("type/anonymous/solo", func(m *ir.Model) {
		m.Anonymous = true
		m.Properties = []ir.Property{{ID: "property/test/q", Name: ir.Naming{Source: "q"}}}
	})
	m := irtest.ModelOf("type/test/M", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "M"}
		m.Base = &ir.TypeRef{Target: "type/test/N"}
		m.Mixins = irtest.RefsTo("type/anonymous/c1", "type/anonymous/c2")
		m.Properties = []ir.Property{{ID: "property/test/y2", Name: ir.Naming{Source: "y"}}}
	})
	single := irtest.ModelOf("type/test/S", func(m *ir.Model) {
		m.Name = ir.Naming{Source: "S"}
		m.Mixins = irtest.RefsTo("type/anonymous/solo")
	})
	var typedNil *ir.Model
	doc := irtest.DocOf(base, inner, c1, c2, solo, m, single, anonAny("type/anonymous/extra"))
	doc.Types["type/test/nil"] = typedNil
	return doc
}

func TestCorrelation_KeysAModelsInPlaceConjunctsAsItsOwn(t *testing.T) {
	t.Parallel()
	assertKeys(t, conjunctsFixture(), map[string]string{
		"property/test/n": "type:N/prop:n", "property/test/x": "type:M/prop:x", "property/test/y1": "", "property/test/y2": "type:M/prop:y",
		"property/test/z": "type:M/prop:z", "type/anonymous/c1": "", "type/anonymous/c2": "", "type/anonymous/c3": "",
		"type/anonymous/extra": "type:M/additional", "type/anonymous/solo": "type:S/allOf", "property/test/q": "type:S/prop:q",
	})
}

// variantsFixture holds a union a discriminator tags, one keyed by its
// variants' names on the wire, and an untagged one.
func variantsFixture() *ir.Document {
	mapped := &ir.Union{ID: "type/test/Mapped", Name: ir.Naming{Source: "Mapped"},
		WireTagged: true,
		Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{
			"b": "type/anonymous/b", "a2": "type/anonymous/a", "a": "type/anonymous/a",
		}},
		Variants: []ir.Variant{
			{Type: ir.TypeRef{Target: "type/anonymous/a"}}, {Type: ir.TypeRef{Target: "type/anonymous/b"}},
			{Name: ir.Naming{Source: "c"}, Type: ir.TypeRef{Target: "type/anonymous/c"}}, {Type: ir.TypeRef{Target: "type/test/Named"}},
		}}
	keyed := &ir.Union{ID: "type/test/Keyed", Name: ir.Naming{Source: "Keyed"}, WireTagged: true,
		Variants: []ir.Variant{
			{WireName: "w", Type: ir.TypeRef{Target: "type/anonymous/w"}}, {Name: ir.Naming{Source: "n"}, Type: ir.TypeRef{Target: "type/anonymous/n"}},
			{Type: ir.TypeRef{Target: "type/anonymous/none"}},
		}}
	untagged := &ir.Union{ID: "type/test/Plain", Name: ir.Naming{Source: "Plain"},
		Variants: []ir.Variant{
			{Name: ir.Naming{Source: "v"}, Type: ir.TypeRef{Target: "type/anonymous/v"}}, {Type: ir.TypeRef{Target: "type/anonymous/bare"}},
		}}
	named := irtest.ModelOf("type/test/Named", func(m *ir.Model) { m.Name = ir.Naming{Source: "Named"} })
	return irtest.DocOf(withAnonymous([]ir.TypeDef{mapped, keyed, untagged, named},
		"type/anonymous/a", "type/anonymous/b", "type/anonymous/c", "type/anonymous/w", "type/anonymous/n", "type/anonymous/none", "type/anonymous/v", "type/anonymous/bare")...)
}

func TestCorrelation_KeysAVariantsTypeByWhatNamesTheVariant(t *testing.T) {
	t.Parallel()
	assertKeys(t, variantsFixture(), map[string]string{
		"type/anonymous/a": "type:Mapped/tag:a", "type/anonymous/b": "type:Mapped/tag:b", "type/anonymous/c": "type:Mapped/variant:c",
		"type/anonymous/w": "type:Keyed/tag:w", "type/anonymous/n": "type:Keyed/tag:n", "type/anonymous/none": "",
		"type/anonymous/v": "type:Plain/variant:v", "type/anonymous/bare": "", "type/test/Named": "type:Named",
	})
}

// sharedFixture holds two models of one name in two namespaces of IDs, and a
// model with no name.
func sharedFixture() *ir.Document {
	pet := func(id ir.TypeID, prop ir.PropertyID, held ir.TypeID) *ir.Model {
		return irtest.ModelOf(id, func(m *ir.Model) {
			m.Name = ir.Naming{Source: "Pet"}
			m.Properties = []ir.Property{{ID: prop, Name: ir.Naming{Source: string(prop)}, Type: ir.TypeRef{Target: held}}}
		})
	}
	return irtest.DocOf(pet("type/one/Pet", "property/one/id", "type/anonymous/one"), pet("type/two/Pet", "property/two/tag", "type/anonymous/two"),
		anonAny("type/anonymous/one"), anonAny("type/anonymous/two"), irtest.ModelOf("type/test/Anon", func(*ir.Model) {}))
}

func TestCorrelation_ASharedKeyKeysNeitherEntityNorWhatTheyName(t *testing.T) {
	t.Parallel()
	facts := irfacts.New(sharedFixture())
	got := keysOf(facts, "type/one/Pet", "type/two/Pet", "property/one/id", "property/two/tag", "type/anonymous/one", "type/test/Anon")
	for id, key := range got {
		assert.Empty(t, key, id)
	}
	_, found := facts.Correlated("type:Pet")
	assert.False(t, found, "a key two entities share names neither")
}

// leastFixture holds two operations naming one anonymous type each, declared
// in either order, and a map naming one type as its key and its value.
func leastFixture(swapped bool) *ir.Document {
	withParam := func(id ir.OperationID, name string) ir.Operation {
		op := namedOp(id, name, "/"+name)
		op.Params = []ir.Parameter{{ID: ir.ParameterID("parameter/test/" + name), Name: ir.Naming{Source: "p"}, Type: ir.TypeRef{Target: "type/anonymous/shared"}}}
		op.Request = &ir.Payload{Contents: []ir.Content{
			{MediaType: "application/json", Type: ir.TypeRef{Target: "type/anonymous/twice"}},
			{MediaType: "application/json", Type: ir.TypeRef{Target: "type/anonymous/twice"}},
		}}
		return op
	}
	pair := &ir.MapT{ID: "type/test/Pair", Name: ir.Naming{Source: "Pair"},
		Key: &ir.TypeRef{Target: "type/anonymous/kv"}, Value: ir.TypeRef{Target: "type/anonymous/kv"}}
	ops := []ir.Operation{withParam("operation/test/b", "b"), withParam("operation/test/a", "a")}
	if swapped {
		slices.Reverse(ops)
	}
	return opsDoc(ops, anonAny("type/anonymous/shared"), anonAny("type/anonymous/twice"), anonAny("type/anonymous/kv"), pair)
}

func TestCorrelation_TakesTheLeastKeyAmongThePositionsNamingAType(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		doc := leastFixture(swapped)
		assertKeys(t, doc, map[string]string{
			"type/anonymous/shared": "route:GET ~1a/arg:p/schema",
			"type/anonymous/twice":  "route:GET ~1a/request/content:application~1json",
			"type/anonymous/kv":     "type:Pair/key",
		})
		id, ok := irfacts.New(doc).Correlated("route:GET ~1a/arg:p/schema")
		assert.True(t, ok)
		assert.Equal(t, "type/anonymous/shared", id)
	}
}

func TestCorrelation_ADocumentWithNothingNamedKeysNothing(t *testing.T) {
	t.Parallel()
	var zero irfacts.DocumentFacts
	_, fromZero := zero.Correlation("type/anonymous/x")
	_, fromNil := irfacts.New(nil).Correlated("type:X")
	var typedNil *ir.Model
	doc := irtest.DocOf(irtest.ModelOf("type/test/M", func(m *ir.Model) {
		m.Properties = []ir.Property{{ID: "property/test/a", Type: ir.TypeRef{Target: "type/test/nil"}}}
	}))
	doc.Types["type/test/nil"] = typedNil
	doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{}}}}
	assert.False(t, fromZero)
	assert.False(t, fromNil)
	assertKeys(t, doc, map[string]string{"type/test/M": "", "property/test/a": "", "type/test/nil": ""})
}
