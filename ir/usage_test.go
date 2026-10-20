package ir_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// useDoc builds a document with one operation and the given types.
func useDoc(op ir.Operation, defs ...ir.TypeDef) *ir.Document {
	doc := compDoc(defs...)
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
	return compModel(id, func(m *ir.Model) {
		for _, p := range ids {
			m.Properties = append(m.Properties, useProp(p))
		}
	})
}

// usageGot reads the usage of every type of doc, leaving out those with none.
func usageGot(facts *ir.Facts, doc *ir.Document) map[ir.TypeID]ir.UsageFlags {
	got := map[ir.TypeID]ir.UsageFlags{}
	for id := range doc.Types {
		if flags := facts.Usage(id); flags != 0 {
			got[id] = flags
		}
	}
	return got
}

const (
	useIn   = ir.UsageInput
	useOut  = ir.UsageOutput
	useErr  = ir.UsageError
	useMulp = ir.UsageMultipart
)

// usageRoot is a document naming "t" in one place, the flags it expects, and
// the other types that carry them: the content type beside a part header.
type usageRoot struct {
	doc  *ir.Document
	want ir.UsageFlags
	also []ir.TypeID
}

// usageRootCases are the places an operation or a service names a type, one
// per rule, each naming "t" there. Every case expects "t" and its also types
// flagged as given, and nothing else in the document.
func usageRootCases() map[string]usageRoot {
	encoded := map[ir.PropID]ir.PartEncoding{"p": {Headers: []ir.Property{useProp("t")}}}
	return map[string]usageRoot{
		"parameter": {useDoc(ir.Operation{Params: []ir.Parameter{{Type: useRef("t")}}}, useType("t")), useIn, nil},
		"request content type": {
			useDoc(ir.Operation{Request: useBody("application/json", "t")}, useType("t")), useIn, nil,
		},
		"request content item": {
			useDoc(ir.Operation{Request: &ir.Payload{Contents: []ir.Content{{Type: useRef("u"), Item: &ir.TypeRef{Target: "t"}}}}},
				useType("t"), useType("u")), useIn, []ir.TypeID{"u"},
		},
		"request file contents": {
			useDoc(ir.Operation{Request: &ir.Payload{Contents: []ir.Content{{Type: useRef("u"), File: &ir.FileInfo{Contents: &ir.TypeRef{Target: "t"}}}}}},
				useType("t"), useType("u")), useIn, []ir.TypeID{"u"},
		},
		"request part header": {
			useDoc(ir.Operation{Request: &ir.Payload{Contents: []ir.Content{{Type: useRef("u"), Encoding: encoded}}}},
				useType("t"), useType("u")), useIn, []ir.TypeID{"u"},
		},
		"request item header": {
			useDoc(ir.Operation{Request: &ir.Payload{Contents: []ir.Content{{Type: useRef("u"), ItemEncoding: &ir.PartEncoding{Headers: []ir.Property{useProp("t")}}}}}},
				useType("t"), useType("u")), useIn, []ir.TypeID{"u"},
		},
		"request stream events": {
			useDoc(ir.Operation{RequestStream: &ir.StreamDetail{Events: &ir.TypeRef{Target: "t"}}}, useType("t")), useIn, nil,
		},
		"request stream initial": {
			useDoc(ir.Operation{RequestStream: &ir.StreamDetail{Initial: &ir.TypeRef{Target: "t"}}}, useType("t")), useIn, nil,
		},
		"response content": {
			useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", "t")}}}, useType("t")), useOut, nil,
		},
		"response header": {
			useDoc(ir.Operation{Responses: []ir.Response{{Headers: []ir.Property{useProp("t")}}}}, useType("t")), useOut, nil,
		},
		"response part header": {
			useDoc(ir.Operation{Responses: []ir.Response{{Payload: &ir.Payload{Contents: []ir.Content{{Type: useRef("u"), Encoding: encoded}}}}}},
				useType("t"), useType("u")), useOut, []ir.TypeID{"u"},
		},
		"response stream events": {
			useDoc(ir.Operation{ResponseStream: &ir.StreamDetail{Events: &ir.TypeRef{Target: "t"}}}, useType("t")), useOut, nil,
		},
		"response stream initial": {
			useDoc(ir.Operation{ResponseStream: &ir.StreamDetail{Initial: &ir.TypeRef{Target: "t"}}}, useType("t")), useOut, nil,
		},
		"error content": {
			useDoc(ir.Operation{Errors: []ir.ErrorCase{{Payload: useBody("application/json", "t")}}}, useType("t")), useErr, nil,
		},
		"error header": {
			useDoc(ir.Operation{Errors: []ir.ErrorCase{{Headers: []ir.Property{useProp("t")}}}}, useType("t")), useErr, nil,
		},
		"common error content": {
			func() *ir.Document {
				doc := compDoc(useType("t"))
				doc.Services = []ir.Service{{CommonErrors: []ir.ErrorCase{{Payload: useBody("application/json", "t")}}}}
				return doc
			}(), useErr, nil,
		},
		"common error header": {
			func() *ir.Document {
				doc := compDoc(useType("t"))
				doc.Services = []ir.Service{{CommonErrors: []ir.ErrorCase{{Headers: []ir.Property{useProp("t")}}}}}
				return doc
			}(), useErr, nil,
		},
	}
}

func TestFacts_UsageRoots(t *testing.T) {
	t.Parallel()
	for name, tc := range usageRootCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := map[ir.TypeID]ir.UsageFlags{"t": tc.want}
			for _, id := range tc.also {
				want[id] = tc.want
			}
			assert.Empty(t, cmp.Diff(want, usageGot(ir.NewFacts(tc.doc), tc.doc)))
		})
	}
}

// usageEdge builds a root type holding "x" through one containment edge, and
// names the other types that edge leaves flagged.
type usageEdge struct {
	build func(root ir.TypeID) ir.TypeDef
	also  []ir.TypeID
}

// usageEdgeCases are the ways a type holds another, one per containment edge.
func usageEdgeCases() map[string]usageEdge {
	return map[string]usageEdge{
		"property": {build: func(root ir.TypeID) ir.TypeDef { return useHolder(root, "x") }},
		"additional value": {build: func(root ir.TypeID) ir.TypeDef {
			return compModel(root, func(m *ir.Model) { m.AdditionalProps = &ir.AdditionalProps{Value: useRef("x")} })
		}},
		"additional key": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return compModel(root, func(m *ir.Model) {
				m.AdditionalProps = &ir.AdditionalProps{Value: useRef("v"), Key: &ir.TypeRef{Target: "x"}}
			})
		}},
		"additional pattern": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return compModel(root, func(m *ir.Model) {
				m.AdditionalProps = &ir.AdditionalProps{
					Value: useRef("v"), Patterns: []ir.PatternProps{{Pattern: "^a", Value: useRef("x")}},
				}
			})
		}},
		"base": {build: func(root ir.TypeID) ir.TypeDef {
			return compModel(root, func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "x"} })
		}},
		"mixin": {build: func(root ir.TypeID) ir.TypeDef {
			return compModel(root, func(m *ir.Model) { m.Mixins = refs("x") })
		}},
		"implements": {build: func(root ir.TypeID) ir.TypeDef {
			return compModel(root, func(m *ir.Model) { m.Implements = refs("x") })
		}},
		"union variant": {build: func(root ir.TypeID) ir.TypeDef {
			return &ir.Union{ID: root, Variants: []ir.Variant{{Type: useRef("x")}}}
		}},
		"list element": {build: func(root ir.TypeID) ir.TypeDef { return &ir.List{ID: root, Elem: useRef("x")} }},
		"map key": {also: []ir.TypeID{"v"}, build: func(root ir.TypeID) ir.TypeDef {
			return &ir.MapT{ID: root, Key: &ir.TypeRef{Target: "x"}, Value: useRef("v")}
		}},
		"map value":     {build: func(root ir.TypeID) ir.TypeDef { return &ir.MapT{ID: root, Value: useRef("x")} }},
		"tuple element": {build: func(root ir.TypeID) ir.TypeDef { return &ir.Tuple{ID: root, Elems: refs("x")} }},
		"tuple rest": {build: func(root ir.TypeID) ir.TypeDef {
			return &ir.Tuple{ID: root, Rest: &ir.TypeRef{Target: "x"}}
		}},
		"scalar base": {build: func(root ir.TypeID) ir.TypeDef { return compScalar(root, "x") }},
	}
}

// TestFacts_UsageSpreadsAlongEveryContainmentEdge gives a response a root type
// holding "x" by each edge in turn. "x" holds "deep", so a flag that reaches
// "x" must go on to the types "x" holds.
func TestFacts_UsageSpreadsAlongEveryContainmentEdge(t *testing.T) {
	t.Parallel()
	for name, edge := range usageEdgeCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", "root")}}},
				edge.build("root"), useHolder("x", "deep"), useType("deep"), useType("v"), useType("unrelated"))

			want := map[ir.TypeID]ir.UsageFlags{"root": useOut, "x": useOut, "deep": useOut}
			for _, id := range edge.also {
				want[id] = useOut
			}
			assert.Empty(t, cmp.Diff(want, usageGot(ir.NewFacts(doc), doc)))
		})
	}
}

func TestFacts_UsageReachesSubtypesOfAPolymorphicBase(t *testing.T) {
	t.Parallel()
	respond := func(defs ...ir.TypeDef) (*ir.Facts, *ir.Document) {
		doc := useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", "base")}}}, defs...)
		return ir.NewFacts(doc), doc
	}
	baseRef := &ir.TypeRef{Target: "base"}
	subOf := func(id ir.TypeID, parent *ir.TypeRef) *ir.Model {
		return compModel(id, func(m *ir.Model) { m.Base = parent })
	}

	t.Run("a discriminated base with no mapping reaches a subtype declared by Base alone", func(t *testing.T) {
		t.Parallel()
		facts, doc := respond(
			compModel("base", func(m *ir.Model) { m.Discriminator = &ir.Discriminator{PropertyName: "kind"} }),
			subOf("sub", baseRef), subOf("grandsub", &ir.TypeRef{Target: "sub"}), compModel("stranger", nil))
		assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"base": useOut, "sub": useOut, "grandsub": useOut}, usageGot(facts, doc))
	})
	t.Run("an abstract base reaches the model that implements it", func(t *testing.T) {
		t.Parallel()
		facts, doc := respond(
			compModel("base", func(m *ir.Model) { m.Abstract = true }),
			compModel("impl", func(m *ir.Model) { m.Implements = refs("base") }))
		assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"base": useOut, "impl": useOut}, usageGot(facts, doc))
	})
	t.Run("a plain base does not reach its subtype", func(t *testing.T) {
		t.Parallel()
		facts, doc := respond(compModel("base", nil), subOf("sub", baseRef))
		assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"base": useOut}, usageGot(facts, doc))
	})
	t.Run("a discriminator mapping alone is no edge", func(t *testing.T) {
		t.Parallel()
		facts, doc := respond(
			compModel("base", func(m *ir.Model) {
				m.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": "mapped"}, Default: "fallback"}
			}),
			compModel("mapped", nil), compModel("fallback", nil))
		assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"base": useOut}, usageGot(facts, doc))
	})
	t.Run("a flag spreads to a subtype's own members", func(t *testing.T) {
		t.Parallel()
		facts, doc := respond(
			compModel("base", func(m *ir.Model) { m.Abstract = true }),
			compModel("impl", func(m *ir.Model) {
				m.Implements = refs("base")
				m.Properties = []ir.Property{useProp("field")}
			}), useType("field"))
		assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"base": useOut, "impl": useOut, "field": useOut}, usageGot(facts, doc))
	})
}

func TestFacts_UsageSwapsSidesForAWebhook(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		Params:    []ir.Parameter{{Type: useRef("param")}},
		Request:   useBody("application/json", "request"),
		Responses: []ir.Response{{Payload: useBody("application/json", "response")}},
		Errors:    []ir.ErrorCase{{Payload: useBody("application/json", "failure")}},
	}
	defs := func() []ir.TypeDef {
		return []ir.TypeDef{useType("param"), useType("request"), useType("response"), useType("failure")}
	}
	want := map[ir.TypeID]ir.UsageFlags{"param": useOut, "request": useOut, "response": useIn, "failure": useErr}

	for name, bindings := range map[string][]ir.HTTPBinding{
		"a named webhook": {{IsWebhook: true, WebhookName: "newPet"}},
		"an unnamed one":  {{IsWebhook: true}},
		"one binding among several": {
			{Method: "POST", URITemplate: "/pets"},
			{IsWebhook: true, WebhookName: "newPet"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hook := op
			hook.Bindings.HTTP = bindings
			doc := useDoc(hook, defs()...)
			assert.Equal(t, want, usageGot(ir.NewFacts(doc), doc))
		})
	}

	t.Run("a path operation keeps its sides", func(t *testing.T) {
		t.Parallel()
		plain := op
		plain.Bindings.HTTP = []ir.HTTPBinding{{Method: "POST", URITemplate: "/pets"}}
		doc := useDoc(plain, defs()...)
		assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"param": useIn, "request": useIn, "response": useOut, "failure": useErr},
			usageGot(ir.NewFacts(doc), doc))
	})
}

func TestFacts_UsageMarksMultipartOnTheBodyTypeOnly(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		op   ir.Operation
		want map[ir.TypeID]ir.UsageFlags
	}{
		"request": {
			ir.Operation{Request: useBody("multipart/form-data", "form")},
			map[ir.TypeID]ir.UsageFlags{"form": useIn | useMulp, "part": useIn},
		},
		"response, in any case": {
			ir.Operation{Responses: []ir.Response{{Payload: useBody("Multipart/Mixed; boundary=x", "form")}}},
			map[ir.TypeID]ir.UsageFlags{"form": useOut | useMulp, "part": useOut},
		},
		"error": {
			ir.Operation{Errors: []ir.ErrorCase{{Payload: useBody("multipart/related", "form")}}},
			map[ir.TypeID]ir.UsageFlags{"form": useErr | useMulp, "part": useErr},
		},
		"a media type that only mentions multipart": {
			ir.Operation{Request: useBody("application/multipart", "form")},
			map[ir.TypeID]ir.UsageFlags{"form": useIn, "part": useIn},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := useDoc(tc.op, useHolder("form", "part"), useType("part"))
			assert.Equal(t, tc.want, usageGot(ir.NewFacts(doc), doc))
		})
	}
}

func TestFacts_UsageOfATypeOnSeveralSidesHasEachFlag(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{
		Request:   useBody("application/json", "t"),
		Responses: []ir.Response{{Payload: useBody("application/json", "t")}},
		Errors:    []ir.ErrorCase{{Payload: useBody("application/json", "t")}},
	}, useType("t"))
	assert.Equal(t, useIn|useOut|useErr, ir.NewFacts(doc).Usage("t"))
}

func TestFacts_UsageOfAnUnreachedOrUnknownTypeIsZero(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{
		Request: useBody("application/json", "ghost"),
		Params:  []ir.Parameter{{Type: ir.TypeRef{}}},
	}, useType("orphan"), useHolder("holder", "ghost"))
	facts := ir.NewFacts(doc)

	assert.Zero(t, facts.Usage("orphan"), "nothing reaches it")
	assert.Zero(t, facts.Usage("ghost"), "the document declares no such type, though an operation names it")
	assert.Zero(t, facts.Usage(""), "an empty ID names nothing")
}

func TestFacts_UsageTerminatesOnAContainmentCycle(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{Request: useBody("application/json", "a")},
		useHolder("a", "b", "a"), useHolder("b", "a"))
	assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"a": useIn, "b": useIn}, usageGot(ir.NewFacts(doc), doc))
}

func TestFacts_UsageReadsEveryOperationOfEveryService(t *testing.T) {
	t.Parallel()
	doc := compDoc(useType("first"), useType("nested"), useType("second"))
	doc.Services = []ir.Service{
		{Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{Request: useBody("application/json", "first")}},
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{Request: useBody("application/json", "nested")}},
			}},
		}}},
		{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{Responses: []ir.Response{{Payload: useBody("application/json", "second")}}}}}}},
	}
	assert.Equal(t, map[ir.TypeID]ir.UsageFlags{"first": useIn, "nested": useIn, "second": useOut},
		usageGot(ir.NewFacts(doc), doc))
}

func TestFacts_UsageToleratesATypedNilEntry(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{Request: useBody("application/json", "nilm")})
	doc.Types["nilm"] = (*ir.Model)(nil)
	assert.Equal(t, useIn, ir.NewFacts(doc).Usage("nilm"), "the registry declares the ID, so it is flagged and holds nothing")
}

func TestFacts_UsageIgnoresPositionsThatNameNoType(t *testing.T) {
	t.Parallel()
	doc := useDoc(ir.Operation{
		Params:         []ir.Parameter{{Type: ir.TypeRef{}}},
		Request:        &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{}, Item: &ir.TypeRef{}, File: &ir.FileInfo{}}}},
		RequestStream:  &ir.StreamDetail{},
		ResponseStream: &ir.StreamDetail{Events: &ir.TypeRef{}},
		Responses:      []ir.Response{{Payload: &ir.Payload{}}, {}},
	}, useType("t"))
	assert.Empty(t, usageGot(ir.NewFacts(doc), doc))
}
