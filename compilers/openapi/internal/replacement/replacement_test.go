// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/replacement"
	"github.com/dexpace/morphic/ir"
)

// mapped is a promoter whose policy reads replacements from x-replaced-by.
func mapped() promotion.Promoter {
	return promotion.New(replacedBy())
}

// replacedBy is a promotion mapping reading replacements from x-replaced-by.
func replacedBy() promotion.ExtensionPromotions {
	return promotion.ExtensionPromotions{Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
		promotion.TargetDeprecationReplacedBy: {{Key: "x-replaced-by"}},
	}}
}

// names is a node's x-replaced-by key, holding value.
func names(value string) ir.Unmodeled {
	return ir.Unmodeled{"openapi:x-replaced-by": {
		Reason: ir.ReasonVendorExtension, Value: ir.RawValue(value), Provenance: ir.Provenance{Pointer: "/x-replaced-by"},
	}}
}

// deprecated is a deprecation naming nothing yet.
func deprecated() *ir.Deprecation { return &ir.Deprecation{} }

// replacementDoc is a document in which each kind of node replacement reads
// is deprecated and names its replacement through value, and each has another
// of its kind to name: headers among them at each place a list of them sits.
func replacementDoc(value string) *ir.Document {
	v2 := ids.DocumentIDNamespace{}.NamedType("/components/schemas/v2")
	old := ids.DocumentIDNamespace{}.NamedType("/components/schemas/old")
	header := func(id ir.PropertyID, name string, unmodeled ir.Unmodeled, dep *ir.Deprecation) ir.Header {
		return ir.Header{ID: id, WireName: name, Unmodeled: unmodeled, Deprecation: dep}
	}
	pair := func(at string) []ir.Header {
		return []ir.Header{header(ir.PropertyID(at+"/old"), "X-Old", names(value), deprecated()), header(ir.PropertyID(at+"/v2"), "v2", nil, nil)}
	}
	return &ir.Document{
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{
			{ID: "operation/old", Name: ir.Naming{Source: "getOld"}, Unmodeled: names(value), Deprecation: deprecated(),
				Params: []ir.Parameter{
					{ID: "parameter/old", Name: ir.Naming{Source: "old"}, Unmodeled: names(value), Deprecation: deprecated()},
					{ID: "parameter/v2", Name: ir.Naming{Source: "v2"}},
				},
				Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
					{Param: "parameter/old", Location: ir.HTTPLocationQuery}, {Param: "parameter/v2", Location: ir.HTTPLocationQuery},
				}}}},
				Request: &ir.Payload{Contents: []ir.Content{{
					ItemEncoding: &ir.PartEncoding{Headers: pair("property/item")},
					Encoding:     map[ir.PropertyID]ir.PartEncoding{"property/part": {Headers: pair("property/part")}},
				}}},
				Responses: []ir.Response{{Headers: []ir.Header{
					header("property/h/old", "X-Old", names(value), deprecated()), header("property/h/v2", "V2", nil, nil),
				}, Payload: &ir.Payload{Contents: []ir.Content{{
					Encoding: map[ir.PropertyID]ir.PartEncoding{"property/part": {Headers: pair("property/response-part")}},
				}}}}},
				Errors: []ir.ErrorCase{{Headers: []ir.Header{
					header("property/e/old", "X-Old", names(value), deprecated()), header("property/e/v2", "v2", nil, nil),
				}}},
				Default: &ir.Response{Headers: []ir.Header{
					header("property/d/old", "X-Old", names(value), deprecated()), header("property/d/v2", "v2", nil, nil),
				}},
			},
			{ID: "operation/v2", Name: ir.Naming{Source: "v2"}},
		}}}}},
		Types: ir.TypeRegistry{
			old: &ir.Model{ID: old, Unmodeled: names(value), Deprecation: deprecated(), Properties: []ir.Property{
				{ID: "property/old", WireName: "old", Unmodeled: names(value), Deprecation: deprecated()},
				{ID: "property/v2", WireName: "v2"},
			}},
			v2:            &ir.Model{ID: v2},
			"type/scalar": &ir.Scalar{ID: "type/scalar"},
		},
		Auth: map[ir.AuthID]ir.AuthScheme{
			"auth/old": {ID: "auth/old", Name: ir.Naming{Source: "old"}, Unmodeled: names(value), Deprecation: deprecated()},
			"auth/v2":  {ID: "auth/v2", Name: ir.Naming{Source: "v2"}},
		},
	}
}

// TestResolve_NamesEachReplacementInItsNodesScope pins how each node's name
// resolves: an operation by operationId, a parameter and a header among its
// siblings, a response's or an encoding's, a property within its model, a
// schema by its components name and a security scheme by its key, each marked
// as promoted.
func TestResolve_NamesEachReplacementInItsNodesScope(t *testing.T) {
	t.Parallel()
	promoter := mapped()
	doc := replacementDoc(`"v2"`)
	assert.Empty(t, replacement.Resolve(promoter, compileddoc.New(doc)))

	operation := doc.Services[0].Groups[0].Operations[0]
	v2 := ids.DocumentIDNamespace{}.NamedType("/components/schemas/v2")
	old := ids.DocumentIDNamespace{}.NamedType("/components/schemas/old")
	for _, tc := range []struct {
		name string
		dep  *ir.Deprecation
		prov ir.Provenance
		want ir.EntityRef
	}{
		{"an operation", operation.Deprecation, operation.Provenance, ir.EntityRef{Operation: "operation/v2"}},
		{"a parameter", operation.Params[0].Deprecation, operation.Params[0].Provenance, ir.EntityRef{Param: "parameter/v2"}},
		{"a response header, in any case", operation.Responses[0].Headers[0].Deprecation, operation.Responses[0].Headers[0].Provenance,
			ir.EntityRef{Property: "property/h/v2"}},
		{"an error case's header", operation.Errors[0].Headers[0].Deprecation, operation.Errors[0].Headers[0].Provenance,
			ir.EntityRef{Property: "property/e/v2"}},
		{"a default's header", operation.Default.Headers[0].Deprecation, operation.Default.Headers[0].Provenance,
			ir.EntityRef{Property: "property/d/v2"}},
		{"a request part's header", operation.Request.Contents[0].Encoding["property/part"].Headers[0].Deprecation,
			operation.Request.Contents[0].Encoding["property/part"].Headers[0].Provenance, ir.EntityRef{Property: "property/part/v2"}},
		{"a stream item's header", operation.Request.Contents[0].ItemEncoding.Headers[0].Deprecation,
			operation.Request.Contents[0].ItemEncoding.Headers[0].Provenance, ir.EntityRef{Property: "property/item/v2"}},
		{"a response part's header", operation.Responses[0].Payload.Contents[0].Encoding["property/part"].Headers[0].Deprecation,
			operation.Responses[0].Payload.Contents[0].Encoding["property/part"].Headers[0].Provenance,
			ir.EntityRef{Property: "property/response-part/v2"}},
		{"a schema", doc.Types[old].Common().Deprecation, doc.Types[old].Common().Provenance, ir.EntityRef{Type: v2}},
		{"a property", doc.Types[old].(*ir.Model).Properties[0].Deprecation,
			doc.Types[old].(*ir.Model).Properties[0].Provenance, ir.EntityRef{Property: "property/v2"}},
		{"a security scheme", doc.Auth["auth/old"].Deprecation, doc.Auth["auth/old"].Provenance, ir.EntityRef{Auth: "auth/v2"}},
	} {
		require.NotNil(t, tc.dep, tc.name)
		assert.Equal(t, []ir.EntityRef{tc.want}, tc.dep.ReplacedBy, tc.name)
		assert.Equal(t, promotion.ExtensionPromotionHeuristic, tc.prov.Inferred, "%s is marked promoted", tc.name)
	}
}

// TestResolve_ANameNamingNothingFillsNothing pins that the names fill the field
// all or none: a list one of whose names names nothing, or one naming a node
// itself, leaves the field unset and is reported at the key.
func TestResolve_ANameNamingNothingFillsNothing(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`["v2","gone"]`, `"old"`, `["getOld"]`, `"X-Old"`} {
		promoter := mapped()
		doc := replacementDoc(value)
		diags := replacement.Resolve(promoter, compileddoc.New(doc))
		require.NotEmpty(t, diags, value)
		for _, diagnostic := range diags {
			assert.Equal(t, diag.DegradedConstruct, diagnostic.Code, value)
			assert.Equal(t, ir.SeverityInfo, diagnostic.Severity, value)
		}
		operation := doc.Services[0].Groups[0].Operations[0]
		assert.Empty(t, operation.Deprecation.ReplacedBy, value)
		assert.Empty(t, operation.Errors[0].Headers[0].Deprecation.ReplacedBy, value)
	}
}

// severalV2 makes "v2" name two entities of each class replacementDoc names it
// for, bar a schema and a property, which one name declares once: another
// operation, a header parameter, a header whose name differs from the other
// only in case in each response, and a security scheme. first places each
// second entity before the first.
func severalV2(doc *ir.Document, first bool) {
	add := func(hs []ir.Header, id ir.PropertyID, name string) []ir.Header {
		header := ir.Header{ID: id, WireName: name}
		if first {
			return append([]ir.Header{header}, hs...)
		}
		return append(hs, header)
	}
	group := &doc.Services[0].Groups[0]
	group.Operations = append(group.Operations, ir.Operation{ID: "operation/v2-again", Name: ir.Naming{Source: "v2"}})
	operation := &group.Operations[0]
	operation.Params = append(operation.Params, ir.Parameter{ID: "parameter/v2-header", Name: ir.Naming{Source: "v2"}})
	bindings := &operation.Bindings.HTTP[0].ParamBindings
	*bindings = append(*bindings, ir.HTTPParamBinding{Param: "parameter/v2-header", Location: ir.HTTPLocationHeader})
	operation.Responses[0].Headers = add(operation.Responses[0].Headers, "property/h/v2-lower", "v2")
	operation.Errors[0].Headers = add(operation.Errors[0].Headers, "property/e/v2-upper", "V2")
	operation.Default.Headers = add(operation.Default.Headers, "property/d/v2-upper", "V2")
	doc.Auth["auth/v2-again"] = ir.AuthScheme{ID: "auth/v2-again", Name: ir.Naming{Source: "v2"}}
}

// TestResolve_ANameNamingSeveralFillsNothing pins that a name matching more
// than one entity of its node's class picks none, in either declaration order,
// and is reported as naming more than one rather than none: two operations,
// parameters, headers whose names differ only in case, or security schemes.
func TestResolve_ANameNamingSeveralFillsNothing(t *testing.T) {
	t.Parallel()
	for _, first := range []bool{false, true} {
		promoter := mapped()
		doc := replacementDoc(`"v2"`)
		severalV2(doc, first)
		diags := replacement.Resolve(promoter, compileddoc.New(doc))

		messages := make([]string, 0, len(diags))
		for _, diagnostic := range diags {
			assert.Equal(t, diag.DegradedConstruct, diagnostic.Code)
			messages = append(messages, diagnostic.Message)
		}
		const several = "replacement \"v2\" names more than one %s, so it does not fill deprecation.replacedBy"
		for _, class := range []string{"operation", "parameter", "header", "header", "header", "security scheme"} {
			assert.Contains(t, messages, fmt.Sprintf(several, class), "first %t", first)
		}
		assert.Len(t, diags, 6, "first %t: the schema and the property each name one", first)

		operation := doc.Services[0].Groups[0].Operations[0]
		at := func(hs []ir.Header, id ir.PropertyID) *ir.Deprecation {
			i := slices.IndexFunc(hs, func(header ir.Header) bool { return header.ID == id })
			require.GreaterOrEqual(t, i, 0, id)
			return hs[i].Deprecation
		}
		for name, dep := range map[string]*ir.Deprecation{
			"operation": operation.Deprecation, "parameter": operation.Params[0].Deprecation,
			"response header": at(operation.Responses[0].Headers, "property/h/old"), "error header": at(operation.Errors[0].Headers, "property/e/old"),
			"default header": at(operation.Default.Headers, "property/d/old"), "security scheme": doc.Auth["auth/old"].Deprecation,
		} {
			assert.Empty(t, dep.ReplacedBy, "first %t: %s", first, name)
		}
	}
}

// TestResolve_AnUnmappedOrUndeprecatedNodeTakesNone pins that a policy naming
// no key reads nothing, and that a name beside no deprecation annotates
// nothing, as a deprecation's other members do.
func TestResolve_AnUnmappedOrUndeprecatedNodeTakesNone(t *testing.T) {
	t.Parallel()
	unmapped := promotion.New(promotion.ExtensionPromotions{})
	doc := replacementDoc(`"v2"`)
	assert.Empty(t, replacement.Resolve(unmapped, compileddoc.New(doc)))
	assert.Empty(t, doc.Services[0].Groups[0].Operations[0].Deprecation.ReplacedBy)

	promoter := mapped()
	doc = replacementDoc(`"v2"`)
	doc.Services[0].Groups[0].Operations[0].Deprecation = nil
	doc.Types[ids.DocumentIDNamespace{}.NamedType("/components/schemas/v2")] = (*ir.Model)(nil)
	assert.Empty(t, replacement.Resolve(promoter, compileddoc.New(doc)))
	assert.Nil(t, doc.Services[0].Groups[0].Operations[0].Deprecation)
}

// TestResolve_StopsAtTheGroupDepthBound holds the walk to ir.MaxGroupDepth: an
// operation nested deeper than any document the compiler builds is not read.
func TestResolve_StopsAtTheGroupDepthBound(t *testing.T) {
	t.Parallel()
	promoter := mapped()
	deep := ir.OperationGroup{Operations: []ir.Operation{
		{ID: "operation/deep", Unmodeled: names(`"v2"`), Deprecation: deprecated()}, {ID: "operation/v2", Name: ir.Naming{Source: "v2"}},
	}}
	for range ir.MaxGroupDepth + 1 {
		deep = ir.OperationGroup{Groups: []ir.OperationGroup{deep}}
	}
	doc := &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{deep}}}}
	assert.Empty(t, replacement.Resolve(promoter, compileddoc.New(doc)))
	for range ir.MaxGroupDepth + 1 {
		deep = deep.Groups[0]
	}
	assert.Empty(t, deep.Operations[0].Deprecation.ReplacedBy)
}

// carrierDoc is a document whose operation's parameter "old", deprecated and
// keeping own, names node as its type, beside a parameter v2 and the schema
// v2, each a replacement it can name.
func carrierDoc(old ir.Parameter, node *ir.Scalar) *ir.Document {
	v2 := ids.DocumentIDNamespace{}.NamedType("/components/schemas/v2")
	old.ID, old.Name, old.Type = "parameter/old", ir.Naming{Source: "old"}, ir.TypeRef{Target: node.ID}
	return &ir.Document{
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "operation/a",
			Params: []ir.Parameter{old, {ID: "parameter/v2", Name: ir.Naming{Source: "v2"}}},
			Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
				{Param: "parameter/old", Location: ir.HTTPLocationQuery}, {Param: "parameter/v2", Location: ir.HTTPLocationQuery},
			}}}},
		}}}}}},
		Types: ir.TypeRegistry{v2: &ir.Model{ID: v2}, node.ID: node},
	}
}

// TestResolve_ACarrierReadsTheKeysOfTheNodeItsPositionBuilt pins where a
// deprecated carrier whose own keys name no replacement reads them: off the
// node its type names, as a schema's, where its declaration built that node,
// at the carrier's pointer or its schema's, or, for a header, at any Header
// Object's schema in any document, and the node holds no deprecation
// (docs/compilers/openapi.md#extension-promotion). A node anywhere else, or
// deprecated itself, lends nothing, and a key the carrier keeps is its own.
func TestResolve_ACarrierReadsTheKeysOfTheNodeItsPositionBuilt(t *testing.T) {
	t.Parallel()
	schema := []ir.EntityRef{{Type: ids.DocumentIDNamespace{}.NamedType("/components/schemas/v2")}}
	at := func(source int, pointer string) ir.Provenance {
		return ir.Provenance{Source: source, Pointer: jsontext.Pointer(pointer)}
	}
	const use = "/paths/~1a/get/responses/200/headers/X"
	for _, tc := range []struct {
		name    string
		header  bool
		carrier ir.Provenance
		node    ir.Provenance
		nodeDep *ir.Deprecation
		own     ir.Unmodeled
		want    []ir.EntityRef
	}{
		{"a node at the carrier's pointer", false, at(0, "/p"), at(0, "/p"), nil, nil, schema},
		{"a node at its schema", false, at(0, "/p"), at(0, "/p/schema"), nil, nil, schema},
		{"a node at its content's schema", false, at(0, "/p"), at(0, "/p/content/application~1json/schema"), nil, nil, schema},
		{"a node beneath its schema", false, at(0, "/p"), at(0, "/p/oneOf/1"), nil, nil, nil},
		{"a node beneath its content", false, at(0, "/p"), at(0, "/p/content/a/b/schema"), nil, nil, nil},
		{"a node beneath another keyword", false, at(0, "/p"), at(0, "/p/allOf/0/schema"), nil, nil, nil},
		{"a node of another document", false, at(0, "/p"), at(1, "/p/schema"), nil, nil, nil},
		{"a carrier of no document", false, at(ir.NoSource, ""), at(ir.NoSource, ""), nil, nil, nil},
		{"a node deprecated itself", false, at(0, "/p"), at(0, "/p/schema"), deprecated(), nil, nil},
		{"a carrier keeping its own key", false, at(0, "/p"), at(0, "/p/schema"), nil, names(`"v2"`),
			[]ir.EntityRef{{Param: "parameter/v2"}}},
		{"a header's node at its own schema", true, at(0, use), at(0, use+"/schema"), nil, nil, schema},
		{"a header's node at a component header's schema", true, at(0, use), at(0, "/components/headers/H/schema"), nil, nil, schema},
		{"a header's node at a component header's content schema", true, at(0, use),
			at(0, "/components/headers/H/content/text~1plain/schema"), nil, nil, schema},
		{"a header's node at a header's schema in another document", true, at(0, use), at(1, "/components/headers/H/schema"), nil, nil, schema},
		{"a parameter's node at a component header's schema", false, at(0, "/p"), at(0, "/components/headers/H/schema"), nil, nil, nil},
		{"a header's node beneath a component header's schema", true, at(0, use), at(0, "/components/headers/H/items/schema"), nil, nil, nil},
		{"a header's node at a component parameter's schema", true, at(0, use), at(0, "/components/parameters/H/schema"), nil, nil, nil},
		{"a header's node at a header's own position", true, at(0, use), at(0, "/components/headers/H"), nil, nil, nil},
		{"a header's node of no document", true, at(0, use), at(ir.NoSource, "/components/headers/H/schema"), nil, nil, nil},
		{"a header's node deprecated itself", true, at(0, use), at(0, "/components/headers/H/schema"), deprecated(), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node := &ir.Scalar{ID: "type/node", Unmodeled: names(`"v2"`), Provenance: tc.node, Deprecation: tc.nodeDep}
			if tc.header {
				doc := headerCarrierDoc(ir.Header{Deprecation: deprecated(), Provenance: tc.carrier, Unmodeled: tc.own}, node)
				assert.Empty(t, replacement.Resolve(mapped(), compileddoc.New(doc)))
				assert.Equal(t, tc.want, doc.Services[0].Groups[0].Operations[0].Responses[0].Headers[0].Deprecation.ReplacedBy)
				return
			}
			doc := carrierDoc(ir.Parameter{Deprecation: deprecated(), Provenance: tc.carrier, Unmodeled: tc.own}, node)
			assert.Empty(t, replacement.Resolve(mapped(), compileddoc.New(doc)))
			assert.Equal(t, tc.want, doc.Services[0].Groups[0].Operations[0].Params[0].Deprecation.ReplacedBy)
		})
	}
}

// headerCarrierDoc is carrierDoc with old, a response header, as the carrier
// naming node.
func headerCarrierDoc(old ir.Header, node *ir.Scalar) *ir.Document {
	doc := carrierDoc(ir.Parameter{}, node)
	old.ID, old.Name, old.WireName, old.Type = "property/old", ir.Naming{Source: "old"}, "old", ir.TypeRef{Target: node.ID}
	operation := &doc.Services[0].Groups[0].Operations[0]
	operation.Responses = []ir.Response{{Headers: []ir.Header{old}}}
	return doc
}

// TestResolve_ACarrierLeavesItsNodesMisreadKeyToTheNode pins that a key the
// node keeps that names no replacement is reported once, by the node, and not
// again by the carrier reading it.
func TestResolve_ACarrierLeavesItsNodesMisreadKeyToTheNode(t *testing.T) {
	t.Parallel()
	node := &ir.Scalar{ID: "type/node", Unmodeled: names(`5`), Provenance: ir.Provenance{Pointer: "/p/schema"}}
	doc := carrierDoc(ir.Parameter{Deprecation: deprecated(), Provenance: ir.Provenance{Pointer: "/p"}}, node)
	diags := replacement.Resolve(mapped(), compileddoc.New(doc))
	require.Len(t, diags, 1, "%v", diags)
	assert.Equal(t, jsontext.Pointer("/x-replaced-by"), diags[0].Provenance.Pointer)
	assert.Empty(t, doc.Services[0].Groups[0].Operations[0].Params[0].Deprecation.ReplacedBy)
}
