// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestParams_ASharedDeclarationLeavesItsListItsBound pins one home per
// declaration where the schema is not written at the position mounting it: a
// parameter and a header reached through a $ref read the pointer their schema
// lowered at, so neither carrier restates the bound its List holds.
func TestParams_ASharedDeclarationLeavesItsListItsBound(t *testing.T) {
	t.Parallel()
	doc, svc, diags := compileService(t, `openapi: 3.1.0
info: {title: t, version: "1"}
components:
  parameters:
    Listed: {name: listed, in: query, schema: {type: array, items: {type: string}, minItems: 1}}
  headers:
    Limited: {schema: {type: array, items: {type: string}, maxItems: 5}}
paths:
  /tags:
    get:
      operationId: listTags
      parameters:
        - {$ref: '#/components/parameters/Listed'}
      responses:
        "200":
          description: ok
          headers:
            X-Limited: {$ref: '#/components/headers/Limited'}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)

	assert.Nil(t, op.Params[0].Constraints)
	listed, ok := doc.Types[op.Params[0].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, listed.Constraints)
	assert.Nil(t, op.Responses[0].Headers[0].Constraints)
	limited, ok := doc.Types[op.Responses[0].Headers[0].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MaxItems: new(int64(5))}, limited.Constraints)
}

// TestParams_CollectionBoundBesideARefReachesTheCarrier pins the two carriers
// this package fills (docs/compilers/openapi.md#constraints): the bound written
// for one use of a shared list reaches the parameter or header holding that use,
// and a schema that lowered to a List of its own keeps its bound there.
func TestParams_CollectionBoundBesideARefReachesTheCarrier(t *testing.T) {
	t.Parallel()
	doc, svc, diags := compileService(t, `openapi: 3.1.0
info: {title: t, version: "1"}
components:
  schemas:
    Tags: {type: array, items: {type: string}}
paths:
  /tags:
    get:
      operationId: listTags
      parameters:
        - {name: beside, in: query, schema: {$ref: '#/components/schemas/Tags', minItems: 1}}
        - {name: plain, in: query, schema: {type: array, items: {type: string}, minItems: 1}}
      responses:
        "200":
          description: ok
          headers:
            X-Beside: {schema: {$ref: '#/components/schemas/Tags', maxItems: 5}}
            X-Plain: {schema: {type: array, items: {type: string}, maxItems: 5}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 2)

	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, op.Params[0].Constraints)
	assert.Empty(t, op.Params[0].Unmodeled, "a bound the parameter holds is not also kept")
	assert.Nil(t, op.Params[1].Constraints, "the List holds its own bound")
	plain, ok := doc.Types[op.Params[1].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, plain.Constraints)

	require.Len(t, op.Responses, 1)
	headers := op.Responses[0].Headers
	require.Len(t, headers, 2)
	assert.Equal(t, &ir.Constraints{MaxItems: new(int64(5))}, headers[0].Constraints)
	assert.Empty(t, headers[0].Unmodeled, "a bound the header holds is not also kept")
	assert.Nil(t, headers[1].Constraints, "the List holds its own bound")
}

func TestParams_SchemaConstraints(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /people:
    get:
      operationId: listPeople
      parameters:
        - {name: age, in: query, schema: {type: integer, maximum: 120}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	c := op.Params[0].Constraints
	require.NotNil(t, c, "param scalar constraints land via annotation.Constraints")
	require.NotNil(t, c.Max)
	assert.Equal(t, ir.BigVal("120"), *c.Max, "numeric bound read at full precision")
}

// TestParams_SchemaAnnotationsReachTheParameter asserts a parameter schema's
// annotations land on the ir.Parameter that carries the position. Only its
// constraints used to survive; the rest were dropped with no diagnostic even
// though Parameter has Docs, Examples and Unmodeled (GitHub #116).
func TestParams_SchemaAnnotationsReachTheParameter(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          schema: "+openapitest.InlineProbeBody+"\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := openapitest.FirstOp(t, svc).Params[0]
	openapitest.AssertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	openapitest.AssertProbeExample(t, p.Examples)
	assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, p.Unmodeled, "openapi:not")
	require.NotNil(t, p.Constraints)
	require.NotNil(t, p.Constraints.MaxLength)
	assert.Equal(t, int64(3), *p.Constraints.MaxLength)
}

// TestParams_SchemaAnnotationsSurviveARefNamingTheSchema pins the parameter
// half of the pointer collision. A $ref can name a parameter's schema pointer,
// which hoists a node there — and components lower before paths, so that node
// always exists by the time the parameter is reached. Reading it as the
// declaration's home cost every parameter in this shape all of its schema
// annotations, in the only order there is.
func TestParams_SchemaAnnotationsSurviveARefNamingTheSchema(t *testing.T) {
	t.Parallel()
	doc, svc, diags := compileService(t,
		"openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"+
			"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          schema: "+openapitest.InlineProbeBody+"\n"+
			"      responses: {\"204\": {description: ok}}\n"+
			"components:\n  schemas:\n"+
			"    Outsider: {$ref: '#/paths/~1x/get/parameters/0/schema'}\n")
	openapitest.RequireNoErrorDiags(t, diags)

	p := openapitest.FirstOp(t, svc).Params[0]
	assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target,
		"the parameter's own type is unchanged by the outside reference")
	openapitest.AssertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, p.Unmodeled, "openapi:not")

	sc, ok := doc.Types["t/anon/paths/~1x/get/parameters/0/schema"].(*ir.Scalar)
	require.True(t, ok, "and the referenced pointer still names the schema written there")
	openapitest.AssertProbeDocsKept(t, sc.Docs)
}

// paramRefInheritSpec gives every parameter the same referent and varies only
// what each writes beside its own $ref: Q declares one of each inheritable
// keyword plus a constraint, and Hop is a bare hop on the way to it.
const paramRefInheritSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      parameters:
        - {name: bare, in: query, schema: {$ref: '#/components/schemas/Q'}}
        - name: override
          in: query
          schema:
            $ref: '#/components/schemas/Q'
            description: USE_SITE
            default: use-site
            deprecated: false
        - {name: chained, in: query, schema: {$ref: '#/components/schemas/Hop'}}
      responses: {"200": {description: ok}}
components:
  schemas:
    Q: {type: string, description: REFERENT, deprecated: true, default: referent, maxLength: 9}
    Hop: {$ref: '#/components/schemas/Q'}
`

// paramsOf indexes an operation's parameters by source name.
func paramsOf(t *testing.T, svc ir.Service) map[string]ir.Parameter {
	t.Helper()
	return openapitest.IndexBy(openapitest.FirstOp(t, svc).Params, func(p ir.Parameter) string { return p.Name.Source })
}

// TestParams_RefSchemaInheritsFromItsReferent covers the referent-fallback half
// of the $ref rule at a parameter (GitHub #131). A parameter whose schema is a
// bare $ref used to carry nothing at all — no description, no deprecation, no
// default — because the position never resolved the target it was holding.
func TestParams_RefSchemaInheritsFromItsReferent(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["bare"]
	assert.Equal(t, "REFERENT", p.Docs.Description, "the referent's description reaches the parameter")
	assert.NotNil(t, p.Deprecation, "and its deprecation")
	require.NotNil(t, p.Default, "and its default")
	assert.Equal(t, "referent", p.Default.Str)
	assert.Nil(t, p.Constraints,
		"but never its constraints: fillPropertyConstraints keeps those use-site-only too")
}

// TestParams_RefSchemaUseSiteWinsOverItsReferent pins the half that already
// worked, so closing the fallback cannot quietly invert the precedence: what is
// written beside the $ref describes this use of the type and outranks the target.
func TestParams_RefSchemaUseSiteWinsOverItsReferent(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["override"]
	assert.Equal(t, "USE_SITE", p.Docs.Description, "the use-site description wins")
	require.NotNil(t, p.Default)
	assert.Equal(t, "use-site", p.Default.Str, "and the use-site default")
	assert.Nil(t, p.Deprecation, "and an explicit deprecated: false suppresses the referent's true")
}

// TestParams_RefSchemaInheritsThroughARefChain pins which resolution the
// fallback must use. Hop writes nothing but its own $ref, so a one-hop referent
// (annotation.At's) would leave the parameter with nothing; only following the
// chain to its end reaches Q.
func TestParams_RefSchemaInheritsThroughARefChain(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["chained"]
	assert.Equal(t, "REFERENT", p.Docs.Description, "one hop reaches Hop, which declares nothing")
	assert.NotNil(t, p.Deprecation)
	require.NotNil(t, p.Default)
	assert.Equal(t, "referent", p.Default.Str)
}

const paramXMLSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      parameters:
        - {name: q, in: query, schema: {type: string, xml: {name: XQ}}}
        - name: c
          in: query
          content:
            application/xml:
              schema: {type: string, xml: {name: XCS}}
      responses: {"200": {description: ok}}
`

// TestParams_SchemaXMLHintsKeptUnderUnmodeled covers the one annotation
// ir.Parameter has no field for (GitHub #124), which used to be dropped with no
// diagnostic at all. It is not inert here: the content-style case binds
// application/xml, the media type OpenAPI §4.8.26 conditions xml on, and the
// binding records that content type.
func TestParams_SchemaXMLHintsKeptUnderUnmodeled(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramXMLSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	cases := []struct {
		param, schemaPtr, want string
	}{
		{"q", "/paths/~1x/get/parameters/0/schema", `{"name":"XQ"}`},
		{"c", "/paths/~1x/get/parameters/1/content/application~1xml/schema", `{"name":"XCS"}`},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			entry, ok := params[tc.param].Unmodeled["openapi:xml"]
			require.True(t, ok, "an xml hint with no Parameter field is kept raw, not dropped")
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, tc.want, string(entry.Value), "kept verbatim")
			assert.Equal(t, jsontext.Pointer(tc.schemaPtr+"/xml"), entry.Provenance.Pointer,
				"located at the xml keyword itself")
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, tc.schemaPtr+"/xml"),
				"xml hints", "and announced once, at the same keyword the entry locates")
		})
	}
}

// TestParams_SchemaXMLHintsStayOnAnOwnedNode is the other half of one home per
// declaration: a parameter schema that hoisted a node of its own already has a
// structural XML field, so preserving a second copy on the parameter would give
// one declaration two homes that can drift.
func TestParams_SchemaXMLHintsStayOnAnOwnedNode(t *testing.T) {
	t.Parallel()
	doc, svc, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - {name: obj, in: query, schema: {type: object, xml: {name: XOBJ}}}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["obj"]
	node, ok := doc.Types[p.Type.Target]
	require.True(t, ok, "the object schema owns a node of its own")
	require.NotNil(t, node.Common().XML)
	assert.Equal(t, "XOBJ", node.Common().XML.Name)
	assert.NotContains(t, p.Unmodeled, "openapi:xml", "so the parameter keeps no second copy")
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"and nothing is announced as homeless")
}

const paramRedactionSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      parameters:
        - {name: q, in: query, schema: {type: string, format: password}}
        - name: c
          in: query
          content:
            text/plain:
              schema: {type: string, format: password}
        - {name: beside, in: query, schema: {$ref: '#/components/schemas/Plain', format: password}}
        - {name: ref, in: query, schema: {$ref: '#/components/schemas/Pw'}}
        - {name: pick, in: query, schema: {type: string, enum: [a, b], format: password}}
        - {name: id, in: query, schema: {type: string, format: uuid}}
      responses: {"200": {description: ok}}
components:
  schemas:
    Pw: {type: string, format: password}
    Plain: {type: string}
`

// TestParams_SchemaRedactionKeptUnderUnmodeled covers the one carrier with no
// redaction field: ir.Parameter has neither Property.Secret nor a node of its
// own, so a `format: password` its schema writes is kept verbatim and announced
// rather than dropped (GitHub #578), at the keyword itself, inline, beside a
// $ref and in a content-style parameter alike.
func TestParams_SchemaRedactionKeptUnderUnmodeled(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRedactionSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	for _, tc := range []struct{ param, schemaPtr string }{
		{"q", "/paths/~1x/get/parameters/0/schema"},
		{"c", "/paths/~1x/get/parameters/1/content/text~1plain/schema"},
		{"beside", "/paths/~1x/get/parameters/2/schema"},
	} {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			entry, ok := params[tc.param].Unmodeled["openapi:format"]
			require.True(t, ok, "a redaction with no Parameter field is kept raw, not dropped")
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, `"password"`, string(entry.Value), "kept verbatim")
			assert.Equal(t, jsontext.Pointer(tc.schemaPtr+"/format"), entry.Provenance.Pointer,
				"located at the format keyword itself")
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, tc.schemaPtr+"/format"),
				"redaction", "and announced once, at the same keyword the entry locates")
		})
	}
	assert.Equal(t, 3, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"one announcement per parameter that keeps it, and none for the others")
}

// TestParams_SchemaRedactionStaysOnANode is the other half: a parameter whose
// type is a Sensitive node, its own or a referent's, keeps no second copy, and
// one writing a format that names a shape keeps nothing.
func TestParams_SchemaRedactionStaysOnANode(t *testing.T) {
	t.Parallel()
	doc, svc, diags := compileService(t, paramRedactionSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	id, ok := params["id"]
	require.True(t, ok)
	assert.NotContains(t, id.Unmodeled, "openapi:format", "a format naming a shape is no redaction")
	for _, name := range []string{"ref", "pick"} {
		p, ok := params[name]
		require.True(t, ok, name)
		node, ok := doc.Types[p.Type.Target]
		require.True(t, ok, "%s: its type is a node", name)
		assert.True(t, node.Common().Sensitive, "%s: the node holds the redaction", name)
		assert.NotContains(t, p.Unmodeled, "openapi:format", "%s: so the parameter keeps no copy", name)
	}
}

const paramVisibilitySpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      parameters:
        - {name: ro, in: query, schema: {type: string, default: dq, readOnly: true}}
        - {name: wo, in: query, schema: {type: string, writeOnly: true}}
        - name: c
          in: query
          content:
            application/xml:
              schema: {type: string, readOnly: true}
      responses: {"200": {description: ok}}
`

// TestParams_SchemaVisibilityKeptUnderUnmodeled covers the residue keywords a
// parameter has no field for. The rule
// docs/compilers/openapi.md#use-bound-annotations lowers readOnly/writeOnly to
// a Visibility, which only ir.Property carries, so at a parameter both reached
// no field and were dropped with no diagnostic at all — the silent loss this
// compiler exists to make impossible.
func TestParams_SchemaVisibilityKeptUnderUnmodeled(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramVisibilitySpec)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	cases := []struct{ param, keyword, schemaPtr string }{
		{"ro", "readOnly", "/paths/~1x/get/parameters/0/schema"},
		{"wo", "writeOnly", "/paths/~1x/get/parameters/1/schema"},
		{"c", "readOnly", "/paths/~1x/get/parameters/2/content/application~1xml/schema"},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			at := tc.schemaPtr + "/" + tc.keyword
			entry, ok := params[tc.param].Unmodeled["openapi:"+tc.keyword]
			require.True(t, ok, "%s has no ir.Parameter field, so it is kept raw, not dropped", tc.keyword)
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, `true`, string(entry.Value), "kept verbatim")
			assert.Equal(t, jsontext.Pointer(at), entry.Provenance.Pointer, "located at the keyword itself")
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at),
				tc.keyword+" has no ir.Parameter home", "and announced once")
		})
	}
}

// TestParams_KeepWhatAnyCarrierKeeps pins the keepers a parameter shares with a
// property. A parameter whose schema owns no node once kept neither the content
// vocabulary nor a $dynamicRef that did not expand, and reported neither, which
// is the silent loss a property never had, as it called them itself.
func TestParams_KeepWhatAnyCarrierKeeps(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      parameters:
        - {name: enc, in: query, schema: {contentEncoding: base64}}
        - {name: dyn, in: query, schema: {$dynamicRef: '#ghost'}}
      responses: {'200': {description: ok}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	require.Contains(t, params["enc"].Unmodeled, "openapi:contentEncoding")
	assert.Equal(t, ir.ReasonNoIRHome, params["enc"].Unmodeled["openapi:contentEncoding"].Reason)
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/paths/~1x/get/parameters/0/schema/contentEncoding"), "content-vocabulary")

	require.Contains(t, params["dyn"].Unmodeled, "openapi:$dynamicRef")
	assert.Equal(t, ir.ReasonDegradedLowering, params["dyn"].Unmodeled["openapi:$dynamicRef"].Reason)
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/paths/~1x/get/parameters/1/schema/$dynamicRef"), "was not expanded")
}

// TestParams_SchemaDefaultIsNotAlsoKeptVerbatim is the other side of that list:
// `default` is a residue keyword too, but Parameter.Default is a real home for
// it, so a verbatim copy beside it would give one declaration two homes.
func TestParams_SchemaDefaultIsNotAlsoKeptVerbatim(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramVisibilitySpec)
	openapitest.RequireNoErrorDiags(t, diags)

	p := paramsOf(t, svc)["ro"]
	require.NotNil(t, p.Default, "the default lands in its own field")
	assert.Equal(t, "dq", p.Default.Str)
	assert.NotContains(t, p.Unmodeled, "openapi:default", "and is not restated verbatim beside it")
	for _, d := range diags {
		assert.NotEqual(t, jsontext.Pointer("/paths/~1x/get/parameters/0/schema/default"), d.Provenance.Pointer,
			"nor announced as homeless; got %+v", d)
	}
}

// TestParams_OwnAnnotationsWinOverTheSchema checks the precedence a parameter
// shares with a header: the parameter object describes this input, its schema
// describes the type, and the more specific of the two wins.
func TestParams_OwnAnnotationsWinOverTheSchema(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - name: q\n          in: query\n          description: PARAM\n"+
			"          x-scope: param\n          schema: {type: string, description: SCHEMA, x-scope: schema}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := openapitest.FirstOp(t, svc).Params[0]
	assert.Equal(t, "PARAM", p.Docs.Description, "the parameter's own description wins")
	raw, ok := p.Unmodeled["openapi:x-scope"]
	require.True(t, ok)
	assert.JSONEq(t, `"param"`, string(raw.Value), "and its own extension overlays the schema's")
}

// TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode pins the half of the
// parameter-visibility rescue that the own-node guard used to swallow. A
// parameter whose schema is an object, an enum or an array hoists a node, and
// neither home held the keywords: recordDeclarationResidue skips the position
// because a parameter is an annotation.HomeCarrier, and ir.Parameter has no
// Visibility field, so they were dropped for exactly the shapes that own a
// node.
func TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      parameters:
        - {name: obj, in: query, schema: {type: object, properties: {a: {type: string}}, readOnly: true}}
        - {name: arr, in: query, schema: {type: array, items: {type: string}, writeOnly: true}}
        - {name: enm, in: query, schema: {type: string, enum: [a, b], readOnly: true}}
      responses: {"200": {description: ok}}
`))
	op := openapitest.FindOp(t, doc, "")
	require.Len(t, op.Params, 3)

	for i, want := range []string{"openapi:readOnly", "openapi:writeOnly", "openapi:readOnly"} {
		param := op.Params[i]
		entry, ok := param.Unmodeled[want]
		require.True(t, ok, "%s: %s kept on the carrier that has no field for it", param.Name.Source, want)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		openapitest.AssertInfoDiagAt(t, diags, string(entry.Provenance.Pointer))
	}
}

// TestParams_RefSiteKeywordsAreKeptOnTheParameter covers the third
// annotation.HomeCarrier position of the $ref-sibling census (GitHub #283).
//
// A parameter whose schema is a $ref resolves straight to the target, so the
// keywords written beside that $ref have no node of their own and no field on
// ir.Parameter either — exactly the shape a property and a header are in, and
// they are kept the same way: verbatim on the carrier, announced at the schema
// position that wrote them.
func TestParams_RefSiteKeywordsAreKeptOnTheParameter(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x:\n    get:\n      operationId: g\n      parameters:\n" +
		"        - {name: q, in: query, schema: {$ref: '#/components/schemas/Base', format: email}}\n" +
		"        - {name: r, in: query, schema: {$ref: '#/components/schemas/Base', minLength: 3}}\n" +
		"      responses: {\"200\": {description: ok}}\n" +
		"components:\n  schemas:\n    Base: {type: string}\n"
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	params := openapitest.IndexBy(openapitest.FirstOp(t, svc).Params, func(p ir.Parameter) string { return p.Name.Source })
	q, ok := params["q"]
	require.True(t, ok)
	entry, ok := q.Unmodeled["openapi:format"]
	require.True(t, ok, "an alias over the target has no Encoding field, so the format is kept")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `"email"`, string(entry.Value))
	openapitest.AssertInfoDiagAt(t, diags, "/paths/~1x/get/parameters/0/schema")

	r, ok := params["r"]
	require.True(t, ok)
	assert.Empty(t, r.Unmodeled, "a bound at the same position always reached ir.Parameter.Constraints")
	require.NotNil(t, r.Constraints)
	assert.Equal(t, int64(3), *r.Constraints.MinLength)
}

// TestParams_CoDeclaredBoundsReachTheParameter covers the parameter carrier for
// a 2020-12 side that declares both of its bound keywords. Each is a keyword the
// source wrote and ir.Constraints has a field for each, so both reach the
// constraints the parameter holds and nothing is kept beside them — at the one
// carrier ir.Parameter owns rather than a node.
//
// The two rows swap which keyword is the tighter across the same magnitudes. A
// reader holding one bound per side answers both rows alike, which is what hid a
// change to the looser keyword from a consumer diffing two revisions (GitHub
// #425).
func TestParams_CoDeclaredBoundsReachTheParameter(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - {name: low, in: query, schema: {type: integer, minimum: 10, exclusiveMinimum: 0}}\n"+
			"        - {name: high, in: query, schema: {type: integer, minimum: 0, exclusiveMinimum: 10}}\n"+
			"        - {name: plain, in: query, schema: {type: integer, minimum: 10}}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	cases := []struct {
		param, wantMin, wantExclMin string
	}{
		{param: "low", wantMin: "10", wantExclMin: "0"},
		{param: "high", wantMin: "0", wantExclMin: "10"},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			c := params[tc.param].Constraints
			require.NotNil(t, c, "both bounds reach the parameter's constraints")
			require.NotNil(t, c.Min)
			require.NotNil(t, c.ExclusiveMin)
			assert.Equal(t, tc.wantMin, c.Min.String(), "minimum as written")
			assert.Equal(t, tc.wantExclMin, c.ExclusiveMin.String(), "exclusiveMinimum as written, beside it")
			assert.Empty(t, params[tc.param].Unmodeled, "with a field apiece there is nothing left to keep")
		})
	}

	assert.Empty(t, params["plain"].Unmodeled,
		"and a side writing one keyword has it in a field, so nothing is restated beside it")
}

// TestParams_ExclusiveModifierWithNoBoundIsKeptOnTheParameter covers the one
// bound keyword that still reaches no field, at the parameter carrier. A 3.0
// exclusiveMinimum modifies the minimum beside it, so one written without a
// minimum modifies nothing and has no bound to become; dropping it would lose a
// declared keyword silently, so the parameter keeps it verbatim.
func TestParams_ExclusiveModifierWithNoBoundIsKeptOnTheParameter(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpecVer("3.0.3",
		"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - {name: bare, in: query, schema: {type: integer, exclusiveMinimum: true}}\n"+
			"      responses: {\"204\": {description: ok}}\n"))
	params := paramsOf(t, svc)

	entry, ok := params["bare"].Unmodeled["openapi:exclusiveMinimum"]
	require.True(t, ok, "kept beside the constraints it did not reach; got %v", params["bare"].Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, "true", string(entry.Value))
	assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0/schema/exclusiveMinimum"), entry.Provenance.Pointer,
		"located at the keyword itself")
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning,
			"/paths/~1x/get/parameters/0/schema"),
		"bounds nothing", "and reading it is what reports on it")
}

// TestParams_RefSiteDocsOverrideTheDeclaration pins the parameter half of
// GitHub #610: a Reference Object's summary and description describe this use of
// the component and override the declaration's own description field by field.
// One component is referenced twice — once with both siblings, once with neither
// — so the override is shown to be per-mount rather than a mutation of the
// declaration.
func TestParams_RefSiteDocsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{$ref: '#/components/parameters/Limit', summary: S, description: D}]
      responses: {"200": {description: ok}}
  /b:
    get:
      operationId: b
      parameters: [{$ref: '#/components/parameters/Limit'}]
      responses: {"200": {description: ok}}
components:
  parameters:
    Limit: {name: limit, in: query, description: declared, schema: {type: integer}}
`
	doc, _, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	withSiblings, ok := openapitest.ParamBySource(openapitest.FindOp(t, doc, "a"), "limit")
	require.True(t, ok)
	assert.Equal(t, "S", withSiblings.Docs.Summary)
	assert.Equal(t, "D", withSiblings.Docs.Description)

	without, ok := openapitest.ParamBySource(openapitest.FindOp(t, doc, "b"), "limit")
	require.True(t, ok)
	assert.Empty(t, without.Docs.Summary, "no sibling means no summary is invented")
	assert.Equal(t, "declared", without.Docs.Description,
		"the other mount keeps the declaration's own description")
}
