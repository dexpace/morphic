package irfacts_test

import (
	"maps"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// useDoc builds a document with one operation and the given types.
func useDoc(op ir.Operation, defs ...ir.TypeDef) *ir.Document {
	doc := irtest.DocOf(defs...)
	doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}}}}
	return doc
}

// useType is a type holding nothing: a place for a flag to land.
func useType(id ir.TypeID) *ir.Primitive { return &ir.Primitive{ID: id, Prim: ir.PrimString} }

func useRef(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

// useBody is a payload of one content of the given media type.
func useBody(mediaType string, id ir.TypeID) *ir.Payload {
	return &ir.Payload{Contents: []ir.Content{{MediaType: mediaType, Type: useRef(id)}}}
}

func useProp(id ir.TypeID) ir.Property { return ir.Property{Type: useRef(id)} }

// useHolder is a model with one property of each of ids.
func useHolder(id ir.TypeID, ids ...ir.TypeID) *ir.Model {
	return irtest.ModelOf(id, func(m *ir.Model) {
		for _, p := range ids {
			m.Properties = append(m.Properties, useProp(p))
		}
	})
}

// usageGot reads the usage of every type of doc, leaving out those with none.
func usageGot(facts *irfacts.Index, doc *ir.Document) map[ir.TypeID]irfacts.UsageFlags {
	got := map[ir.TypeID]irfacts.UsageFlags{}
	for id := range doc.Types {
		if flags := facts.Usage(id); flags != 0 {
			got[id] = flags
		}
	}
	return got
}

// usageOf derives the usage of every type of doc.
func usageOf(doc *ir.Document) map[ir.TypeID]irfacts.UsageFlags {
	return usageGot(irfacts.New(doc), doc)
}

const (
	useIn   = irfacts.UsageInput
	useOut  = irfacts.UsageOutput
	useErr  = irfacts.UsageError
	useMulp = irfacts.UsageMultipart
)

// usageRoot is a document naming "t" in one place, the flags it expects, and
// the other types that carry them: the content type beside a part header, or
// a header's own type beside what its encoding decodes to.
type usageRoot struct {
	doc  *ir.Document
	want irfacts.UsageFlags
	also []ir.TypeID
}

// usageDocWith builds a document declaring defs, and shapes it with mutate.
func usageDocWith(mutate func(*ir.Document), defs ...ir.TypeDef) *ir.Document {
	doc := irtest.DocOf(defs...)
	mutate(doc)
	return doc
}

// usageMessageDoc builds a document with message "msg", whose payload names
// "t", and one operation bound to it by b.
func usageMessageDoc(b *ir.MessageBinding) *ir.Document {
	doc := useDoc(ir.Operation{Bindings: ir.OpBindings{Message: b}}, useType("t"))
	doc.Messages = map[ir.MessageID]ir.Message{"msg": {ID: "msg", Payload: *useBody("application/json", "t")}}
	return doc
}

// usageRootCases are the places a root names a type, one per rule, each naming
// "t" there. Every case expects "t" and its also types flagged as given, and
// nothing else in the document.
func usageRootCases() map[string]usageRoot {
	cases := map[string]usageRoot{
		"parameter": {useDoc(ir.Operation{Params: []ir.Parameter{{Type: useRef("t")}}}, useType("t")), useIn, nil},
		"request stream events": {
			useDoc(ir.Operation{RequestStream: &ir.StreamDetail{Events: &ir.TypeRef{Target: "t"}}}, useType("t")), useIn, nil,
		},
		"request stream initial": {
			useDoc(ir.Operation{RequestStream: &ir.StreamDetail{Initial: &ir.TypeRef{Target: "t"}}}, useType("t")), useIn, nil,
		},
		"rpc input type": {
			useDoc(ir.Operation{Bindings: ir.OpBindings{RPC: &ir.RPCBinding{InputType: &ir.TypeRef{Target: "t"}}}}, useType("t")), useIn, nil,
		},
		"response header": {
			useDoc(ir.Operation{Responses: []ir.Response{{Headers: []ir.Property{useProp("t")}}}}, useType("t")), useOut, nil,
		},
		"response header encoding schema": {
			useDoc(ir.Operation{Responses: []ir.Response{{Headers: []ir.Property{
				{Type: useRef("u"), Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "t"}}},
			}}}}, useType("t"), useType("u")), useOut, []ir.TypeID{"u"},
		},
		"response stream events": {
			useDoc(ir.Operation{ResponseStream: &ir.StreamDetail{Events: &ir.TypeRef{Target: "t"}}}, useType("t")), useOut, nil,
		},
		"response stream initial": {
			useDoc(ir.Operation{ResponseStream: &ir.StreamDetail{Initial: &ir.TypeRef{Target: "t"}}}, useType("t")), useOut, nil,
		},
		"long-running polling type": {
			useDoc(ir.Operation{LongRunning: &ir.LongRunning{PollingType: &ir.TypeRef{Target: "t"}}}, useType("t")), useOut, nil,
		},
		"long-running final type": {
			useDoc(ir.Operation{LongRunning: &ir.LongRunning{FinalType: &ir.TypeRef{Target: "t"}}}, useType("t")), useOut, nil,
		},
		"error header": {
			useDoc(ir.Operation{Errors: []ir.ErrorCase{{Headers: []ir.Property{useProp("t")}}}}, useType("t")), useOut | useErr, nil,
		},
		"common error header": {
			usageDocWith(func(doc *ir.Document) {
				doc.Services = []ir.Service{{CommonErrors: []ir.ErrorCase{{Headers: []ir.Property{useProp("t")}}}}}
			}, useType("t")), useOut | useErr, nil,
		},
		"channel parameter": {
			usageDocWith(func(doc *ir.Document) {
				doc.Channels = map[ir.ChannelID]ir.Channel{"ch": {Params: []ir.Parameter{{Type: useRef("t")}}}}
			}, useType("t")), useIn, nil,
		},
		"auth credentials": {
			usageDocWith(func(doc *ir.Document) {
				doc.Auth = map[ir.AuthID]ir.AuthScheme{"key": {Credentials: &ir.TypeRef{Target: "t"}}}
			}, useType("t")), useIn, nil,
		},
		"message headers": {
			func() *ir.Document {
				doc := usageMessageDoc(&ir.MessageBinding{Direction: ir.MsgDirectionReceive, Messages: []ir.MessageID{"msg"}})
				doc.Messages["msg"] = ir.Message{ID: "msg", Headers: &ir.TypeRef{Target: "t"}}
				return doc
			}(), useOut, nil,
		},
	}
	maps.Copy(cases, usagePayloadCases())
	return cases
}

// usagePayloadCases name "t" at each place a payload names a type, under each
// root a payload has.
func usagePayloadCases() map[string]usageRoot {
	encoded := map[ir.PropID]ir.PartEncoding{"p": {Headers: []ir.Property{useProp("t")}}}
	contents := map[string]ir.Content{
		"content type":  {Type: useRef("t")},
		"content item":  {Type: useRef("u"), Item: &ir.TypeRef{Target: "t"}},
		"file contents": {Type: useRef("u"), File: &ir.FileInfo{Contents: &ir.TypeRef{Target: "t"}}},
		"part header":   {Type: useRef("u"), Encoding: encoded},
		"item header":   {Type: useRef("u"), ItemEncoding: &ir.PartEncoding{Headers: []ir.Property{useProp("t")}}},
	}
	roots := map[string]struct {
		place func(*ir.Payload) *ir.Document
		want  irfacts.UsageFlags
	}{
		"request":  {func(p *ir.Payload) *ir.Document { return useDoc(ir.Operation{Request: p}) }, useIn},
		"response": {func(p *ir.Payload) *ir.Document { return useDoc(ir.Operation{Responses: []ir.Response{{Payload: p}}}) }, useOut},
		"error":    {func(p *ir.Payload) *ir.Document { return useDoc(ir.Operation{Errors: []ir.ErrorCase{{Payload: p}}}) }, useOut | useErr},
		"common error": {func(p *ir.Payload) *ir.Document {
			doc := irtest.DocOf()
			doc.Services = []ir.Service{{CommonErrors: []ir.ErrorCase{{Payload: p}}}}
			return doc
		}, useOut | useErr},
		"sent message": {func(p *ir.Payload) *ir.Document {
			doc := usageMessageDoc(&ir.MessageBinding{Direction: ir.MsgDirectionSend, Messages: []ir.MessageID{"msg"}})
			doc.Messages["msg"] = ir.Message{ID: "msg", Payload: *p}
			return doc
		}, useIn},
	}
	cases := map[string]usageRoot{}
	for rootName, root := range roots {
		for contentName, c := range contents {
			doc := root.place(&ir.Payload{Contents: []ir.Content{c}})
			doc.Types["t"], doc.Types["u"] = useType("t"), useType("u")
			var also []ir.TypeID
			if c.Type.Target == "u" {
				also = []ir.TypeID{"u"}
			}
			cases[rootName+" "+contentName] = usageRoot{doc, root.want, also}
		}
	}
	return cases
}

func TestIndex_UsageRoots(t *testing.T) {
	t.Parallel()
	for name, tc := range usageRootCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := map[ir.TypeID]irfacts.UsageFlags{"t": tc.want}
			for _, id := range tc.also {
				want[id] = tc.want
			}
			assert.Empty(t, cmp.Diff(want, usageOf(tc.doc)))
		})
	}
}

func TestIndex_UsageOfAMessageFollowsItsBinding(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		binding *ir.MessageBinding
		want    irfacts.UsageFlags
	}{
		"sent":     {&ir.MessageBinding{Direction: ir.MsgDirectionSend, Messages: []ir.MessageID{"msg"}}, useIn},
		"received": {&ir.MessageBinding{Direction: ir.MsgDirectionReceive, Messages: []ir.MessageID{"msg"}}, useOut},
		"the reply to a sent one": {
			&ir.MessageBinding{Direction: ir.MsgDirectionSend, Reply: &ir.Reply{Messages: []ir.MessageID{"msg"}}}, useOut,
		},
		"the reply to a received one": {
			&ir.MessageBinding{Direction: ir.MsgDirectionReceive, Reply: &ir.Reply{Messages: []ir.MessageID{"msg"}}}, useIn,
		},
		"bound with no direction":       {&ir.MessageBinding{Messages: []ir.MessageID{"msg"}}, useIn | useOut},
		"named by no binding":           {&ir.MessageBinding{Direction: ir.MsgDirectionSend}, 0},
		"named but not in the registry": {&ir.MessageBinding{Direction: ir.MsgDirectionSend, Messages: []ir.MessageID{"gone"}}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, irfacts.New(usageMessageDoc(tc.binding)).Usage("t"))
		})
	}
}

// usageEdge builds a root type holding "x" through one edge of the type graph,
// and names the other types that edge leaves flagged.
type usageEdge struct {
	build func(root ir.TypeID) ir.TypeDef
	also  []ir.TypeID
}

// usageEdgeCases are the ways a type holds or includes another, one per edge.
func usageEdgeCases() map[string]usageEdge {
	return map[string]usageEdge{
		"property": {build: func(root ir.TypeID) ir.TypeDef { return useHolder(root, "x") }},
		"additional value": {build: func(root ir.TypeID) ir.TypeDef {
			return irtest.ModelOf(root, func(m *ir.Model) { m.AdditionalProps = &ir.AdditionalProps{Value: useRef("x")} })
		}},
		"additional key": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return irtest.ModelOf(root, func(m *ir.Model) {
				m.AdditionalProps = &ir.AdditionalProps{Value: useRef("v"), Key: &ir.TypeRef{Target: "x"}}
			})
		}},
		"additional pattern": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return irtest.ModelOf(root, func(m *ir.Model) {
				m.AdditionalProps = &ir.AdditionalProps{
					Value: useRef("v"), Patterns: []ir.PatternProps{{Pattern: "^a", Value: useRef("x")}},
				}
			})
		}},
		"base": {build: func(root ir.TypeID) ir.TypeDef {
			return irtest.ModelOf(root, func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "x"} })
		}},
		"mixin": {build: func(root ir.TypeID) ir.TypeDef {
			return irtest.ModelOf(root, func(m *ir.Model) { m.Mixins = irtest.RefsTo("x") })
		}},
		"implements": {build: func(root ir.TypeID) ir.TypeDef {
			return irtest.ModelOf(root, func(m *ir.Model) { m.Implements = irtest.RefsTo("x") })
		}},
		"union variant": {build: func(root ir.TypeID) ir.TypeDef {
			return &ir.Union{ID: root, Variants: []ir.Variant{{Type: useRef("x")}}}
		}},
		"list element": {build: func(root ir.TypeID) ir.TypeDef { return &ir.List{ID: root, Elem: useRef("x")} }},
		"map key": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return &ir.MapT{ID: root, Key: &ir.TypeRef{Target: "x"}, Value: useRef("v")}
		}},
		"map value":     {build: func(root ir.TypeID) ir.TypeDef { return &ir.MapT{ID: root, Value: useRef("x")} }},
		"tuple element": {build: func(root ir.TypeID) ir.TypeDef { return &ir.Tuple{ID: root, Elems: irtest.RefsTo("x")} }},
		"tuple rest": {build: func(root ir.TypeID) ir.TypeDef {
			return &ir.Tuple{ID: root, Rest: &ir.TypeRef{Target: "x"}}
		}},
		"scalar base": {build: func(root ir.TypeID) ir.TypeDef { return irtest.ScalarOf(root, "x") }},
		"property encoding schema": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return irtest.ModelOf(root, func(m *ir.Model) {
				m.Properties = []ir.Property{{Type: useRef("v"), Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "x"}}}}
			})
		}},
		"scalar encoding schema": {build: func(root ir.TypeID) ir.TypeDef {
			return &ir.Scalar{ID: root, Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "x"}}}
		}},
		"list encoding schema": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return &ir.List{ID: root, Elem: useRef("v"), Encoding: &ir.Encoding{Schema: &ir.TypeRef{Target: "x"}}}
		}},
	}
}

// TestIndex_UsageSpreadsAlongEveryEdge gives a response a root type holding "x"
// by each edge in turn. "x" holds "deep", so a flag that reaches "x" must go on
// to the types "x" holds.
func TestIndex_UsageSpreadsAlongEveryEdge(t *testing.T) {
	t.Parallel()
	for name, edge := range usageEdgeCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", "root")}}},
				edge.build("root"), useHolder("x", "deep"), useType("deep"), useType("v"), useType("unrelated"))

			want := map[ir.TypeID]irfacts.UsageFlags{"root": useOut, "x": useOut, "deep": useOut}
			for _, id := range edge.also {
				want[id] = useOut
			}
			assert.Empty(t, cmp.Diff(want, usageOf(doc)))
		})
	}
}

// usagePets is a discriminated base "pet" with subtypes "cat" and "dog", "lion"
// below "cat", and "kitten" composing "cat" through the alias "catAlias".
func usagePets(mutate func(id ir.TypeID, m *ir.Model)) []ir.TypeDef {
	model := func(id ir.TypeID, base ir.TypeID) *ir.Model {
		return irtest.ModelOf(id, func(m *ir.Model) {
			if base != "" {
				m.Base = &ir.TypeRef{Target: base}
			}
			if mutate != nil {
				mutate(id, m)
			}
		})
	}
	pet := model("pet", "")
	pet.Discriminator = &ir.Discriminator{PropertyName: "kind"}
	return []ir.TypeDef{pet, model("cat", "pet"), model("dog", "pet"), model("lion", "cat"),
		irtest.ScalarOf("catAlias", "cat"), model("kitten", "catAlias")}
}

func TestIndex_UsageReachesSubtypesOfAPolymorphicBaseOnlyWhereItIsAValue(t *testing.T) {
	t.Parallel()
	respond := func(root ir.TypeID, defs ...ir.TypeDef) map[ir.TypeID]irfacts.UsageFlags {
		return usageOf(useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", root)}}}, defs...))
	}
	all := map[ir.TypeID]irfacts.UsageFlags{"pet": useOut, "cat": useOut, "dog": useOut, "lion": useOut, "catAlias": useOut, "kitten": useOut}

	assert.Equal(t, all, respond("pet", usagePets(nil)...), "a value of the base may be any subtype, at any depth")
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"cat": useOut, "pet": useOut}, respond("cat", usagePets(nil)...),
		"a subtype includes its base's members, which does not make it a sibling")
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"kitten": useOut, "catAlias": useOut, "cat": useOut, "pet": useOut},
		respond("kitten", usagePets(nil)...), "composing through an alias reaches no sibling either")

	viaAlias := append(usagePets(nil), irtest.ScalarOf("petAlias", "pet"))
	wantViaAlias := map[ir.TypeID]irfacts.UsageFlags{"petAlias": useOut}
	maps.Copy(wantViaAlias, all)
	assert.Equal(t, wantViaAlias, respond("petAlias", viaAlias...), "a reference through an alias is a value of the base")

	abstract := []ir.TypeDef{
		irtest.ModelOf("shape", func(m *ir.Model) { m.Abstract = true }),
		irtest.ModelOf("circle", func(m *ir.Model) {
			m.Implements = irtest.RefsTo("shape")
			m.Properties = []ir.Property{useProp("radius")}
		}),
		useType("radius"),
	}
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"shape": useOut, "circle": useOut, "radius": useOut}, respond("shape", abstract...),
		"an abstract base reaches what implements it, and that type's own members")

	plain := []ir.TypeDef{irtest.ModelOf("base", nil), irtest.ModelOf("sub", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "base"} })}
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"base": useOut}, respond("base", plain...), "a plain base names its own shape alone")

	mapped := []ir.TypeDef{
		irtest.ModelOf("base", func(m *ir.Model) {
			m.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": "mapped"}, Default: "fallback"}
		}),
		irtest.ModelOf("mapped", nil), irtest.ModelOf("fallback", nil),
	}
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"base": useOut}, respond("base", mapped...),
		"a discriminator mapping routes to types; only a subtype is one a value of the base may be")
}

func TestIndex_UsageOfAFieldArgumentIsInput(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", "query")}}},
		irtest.ModelOf("query", func(m *ir.Model) {
			m.Properties = []ir.Property{{Type: useRef("result"), Args: []ir.Parameter{{Type: useRef("filter")}}}}
		}),
		useType("result"), useHolder("filter", "clause"), useType("clause"))
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"query": useOut, "result": useOut, "filter": useIn, "clause": useIn}, usageOf(doc),
		"the client writes a field's arguments to select it, whatever it reads back")
}

// TestIndex_UsageIgnoresAReferenceToNoType pins that a reference to a type the
// document does not declare spreads no usage: not from an operation's payload,
// a usage root, nor from a field's argument. "a" sorts first, so a lookup that
// answered an undeclared type with some vertex of its own would reach it.
func TestIndex_UsageIgnoresAReferenceToNoType(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", "ghost")}}},
		useType("a"))
	assert.Empty(t, usageOf(doc), "a payload naming no type uses none")

	doc = useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", "query")}}},
		useType("a"),
		irtest.ModelOf("query", func(m *ir.Model) {
			m.Properties = []ir.Property{{Type: useRef("result"), Args: []ir.Parameter{{Type: useRef("phantom")}}}}
		}),
		useType("result"))
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"query": useOut, "result": useOut}, usageOf(doc),
		"an argument naming no type uses none")
}

func TestIndex_UsageSwapsSidesForAnOperationTheAPIStarts(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:        "op",
		Params:    []ir.Parameter{{Type: useRef("param")}},
		Request:   useBody("application/json", "request"),
		Responses: []ir.Response{{Payload: useBody("application/json", "response")}},
		Errors:    []ir.ErrorCase{{Payload: useBody("application/json", "failure")}},
	}
	defs := func() []ir.TypeDef {
		return []ir.TypeDef{useType("param"), useType("request"), useType("response"), useType("failure")}
	}
	inbound := map[ir.TypeID]irfacts.UsageFlags{"param": useOut, "request": useOut, "response": useIn, "failure": useIn | useErr}
	outbound := map[ir.TypeID]irfacts.UsageFlags{"param": useIn, "request": useIn, "response": useOut, "failure": useOut | useErr}

	for name, tc := range map[string]struct {
		bindings []ir.HTTPBinding
		want     map[ir.TypeID]irfacts.UsageFlags
	}{
		"a named webhook":           {[]ir.HTTPBinding{{IsWebhook: true, WebhookName: "newPet"}}, inbound},
		"an unnamed one":            {[]ir.HTTPBinding{{IsWebhook: true}}, inbound},
		"one binding among several": {[]ir.HTTPBinding{{Method: "POST", URITemplate: "/pets"}, {IsWebhook: true}}, inbound},
		"a path operation":          {[]ir.HTTPBinding{{Method: "POST", URITemplate: "/pets"}}, outbound},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hook := op
			hook.Bindings.HTTP = tc.bindings
			assert.Equal(t, tc.want, usageOf(useDoc(hook, defs()...)))
		})
	}

	t.Run("a callback, however deep its parent", func(t *testing.T) {
		t.Parallel()
		parent := ir.Operation{ID: "parent", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "POST", URITemplate: "/subscribe",
			Callbacks: []ir.Callback{{Expression: "{$request.body#/url}", Operations: []ir.OpID{"op"}}},
		}}}}
		callback := op
		callback.Bindings.HTTP = []ir.HTTPBinding{{Method: "POST", URITemplate: "{$request.body#/url}"}}
		doc := irtest.DocOf(defs()...)
		doc.Services = []ir.Service{{Groups: []ir.OperationGroup{
			{Operations: []ir.Operation{callback}},
			{Groups: []ir.OperationGroup{{Operations: []ir.Operation{parent}}}},
		}}}
		assert.Equal(t, inbound, usageOf(doc))
	})

	t.Run("a webhook's callback is no less inbound", func(t *testing.T) {
		t.Parallel()
		callback := op
		callback.Bindings.HTTP = []ir.HTTPBinding{{IsWebhook: true, Callbacks: []ir.Callback{{Operations: []ir.OpID{"op"}}}}}
		assert.Equal(t, inbound, usageOf(useDoc(callback, defs()...)))
	})
}

func TestIndex_UsageMarksMultipartOnTheBodyTypeAndWhatItAliases(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		op   ir.Operation
		want map[ir.TypeID]irfacts.UsageFlags
	}{
		"request": {
			ir.Operation{Request: useBody("multipart/form-data", "form")},
			map[ir.TypeID]irfacts.UsageFlags{"form": useIn | useMulp, "part": useIn},
		},
		"response, in any case": {
			ir.Operation{Responses: []ir.Response{{Payload: useBody("Multipart/Mixed; boundary=x", "form")}}},
			map[ir.TypeID]irfacts.UsageFlags{"form": useOut | useMulp, "part": useOut},
		},
		"error": {
			ir.Operation{Errors: []ir.ErrorCase{{Payload: useBody("multipart/related", "form")}}},
			map[ir.TypeID]irfacts.UsageFlags{"form": useOut | useErr | useMulp, "part": useOut | useErr},
		},
		"a media type that only mentions multipart": {
			ir.Operation{Request: useBody("application/multipart", "form")},
			map[ir.TypeID]irfacts.UsageFlags{"form": useIn, "part": useIn},
		},
		"through a chain of aliases": {
			ir.Operation{Request: useBody("multipart/form-data", "outer")},
			map[ir.TypeID]irfacts.UsageFlags{"outer": useIn | useMulp, "inner": useIn | useMulp, "form": useIn | useMulp, "part": useIn},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := useDoc(tc.op, useHolder("form", "part"), useType("part"), irtest.ScalarOf("outer", "inner"), irtest.ScalarOf("inner", "form"))
			assert.Equal(t, tc.want, usageOf(doc))
		})
	}

	t.Run("a type the document does not declare is not marked", func(t *testing.T) {
		t.Parallel()
		doc := useDoc(ir.Operation{Request: &ir.Payload{Contents: []ir.Content{
			{MediaType: "multipart/form-data", Type: useRef("ghost")},
			{MediaType: "multipart/form-data", Type: useRef("toGhost")},
		}}}, irtest.ScalarOf("toGhost", "elsewhere"))
		facts := irfacts.New(doc)
		assert.Equal(t, useIn|useMulp, facts.Usage("toGhost"))
		assert.Zero(t, facts.Usage("ghost"), "an undeclared body type")
		assert.Zero(t, facts.Usage("elsewhere"), "an undeclared alias target")
	})

	t.Run("an alias cycle ends", func(t *testing.T) {
		t.Parallel()
		doc := useDoc(ir.Operation{Request: useBody("multipart/form-data", "a")}, irtest.ScalarOf("a", "b"), irtest.ScalarOf("b", "a"))
		assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"a": useIn | useMulp, "b": useIn | useMulp}, usageOf(doc))
	})
}

func TestIndex_UsageOfATypeOnSeveralSidesHasEachFlag(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{
		Request:   useBody("application/json", "t"),
		Responses: []ir.Response{{Payload: useBody("application/json", "t")}},
		Errors:    []ir.ErrorCase{{Payload: useBody("application/json", "t")}},
	}, useType("t"))
	assert.Equal(t, useIn|useOut|useErr, irfacts.New(doc).Usage("t"))
}

func TestIndex_UsageOfAnUnreachedOrUnknownTypeIsZero(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{
		Request: useBody("application/json", "ghost"),
		Params:  []ir.Parameter{{Type: ir.TypeRef{}}},
	}, useType("orphan"), useHolder("holder", "ghost"))
	facts := irfacts.New(doc)

	assert.Zero(t, facts.Usage("orphan"), "nothing reaches it")
	assert.Zero(t, facts.Usage("ghost"), "the document declares no such type, though an operation names it")
	assert.Zero(t, facts.Usage(""), "an empty ID names nothing")
}

func TestIndex_UsageTerminatesOnACycle(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{Request: useBody("application/json", "a")},
		useHolder("a", "b", "a"), useHolder("b", "a"))
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"a": useIn, "b": useIn}, usageOf(doc))
}

func TestIndex_UsageReadsEveryOperationOfEveryService(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(useType("first"), useType("nested"), useType("second"))
	doc.Services = []ir.Service{
		{Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{Request: useBody("application/json", "first")}},
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{Request: useBody("application/json", "nested")}},
			}},
		}}},
		{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{Responses: []ir.Response{{Payload: useBody("application/json", "second")}}}}}}},
	}
	assert.Equal(t, map[ir.TypeID]irfacts.UsageFlags{"first": useIn, "nested": useIn, "second": useOut}, usageOf(doc))
}

func TestIndex_UsageToleratesATypedNilEntry(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{Request: useBody("application/json", "nilm")})
	doc.Types["nilm"] = (*ir.Model)(nil)
	assert.Equal(t, useIn, irfacts.New(doc).Usage("nilm"), "the registry declares the ID, so it is flagged and holds nothing")
}

func TestIndex_UsageIgnoresPositionsThatNameNoType(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{
		Params:         []ir.Parameter{{Type: ir.TypeRef{}}},
		Request:        &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{}, Item: &ir.TypeRef{}, File: &ir.FileInfo{}}}},
		RequestStream:  &ir.StreamDetail{},
		ResponseStream: &ir.StreamDetail{Events: &ir.TypeRef{}},
		Responses:      []ir.Response{{Payload: &ir.Payload{}}, {Headers: []ir.Property{{Encoding: &ir.Encoding{}}}}},
		LongRunning:    &ir.LongRunning{},
		Bindings:       ir.OpBindings{RPC: &ir.RPCBinding{}, Message: &ir.MessageBinding{Reply: &ir.Reply{}}},
	}, useType("t"))
	doc.Auth = map[ir.AuthID]ir.AuthScheme{"key": {}}
	assert.Empty(t, usageOf(doc))
}

// TestUsage_ADefaultTravelsAsOutputAndNoError pins that the payload and a header
// of an operation's Default are the response side's, and carry no error flag:
// the IR does not class a default as a failure (ir-design §7.2), so the type
// reads back as plain output. Bound as a webhook, the sides swap and the same
// types are input.
func TestUsage_ADefaultTravelsAsOutputAndNoError(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID: "op",
		Default: &ir.Response{
			Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 0, To: 0}}},
			Payload:    useBody("application/json", "t"),
			Headers:    []ir.Property{useProp("h")},
		},
	}
	defs := func() []ir.TypeDef { return []ir.TypeDef{irtest.ModelOf("t", nil), useType("h")} }

	for name, tc := range map[string]struct {
		bindings []ir.HTTPBinding
		want     irfacts.UsageFlags
	}{
		"a path operation": {[]ir.HTTPBinding{{Method: "GET", URITemplate: "/pets"}}, useOut},
		"a webhook":        {[]ir.HTTPBinding{{IsWebhook: true}}, useIn},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bound := op
			bound.Bindings.HTTP = tc.bindings
			doc := useDoc(bound, defs()...)
			facts := irfacts.New(doc)

			assert.Equal(t, tc.want, facts.Usage("t"), "the Default's payload type, with no error flag")
			assert.Equal(t, tc.want, facts.Usage("h"), "the Default's header type, with no error flag")
		})
	}
}
