// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	operation := openapitest.FirstOperation(t, svc)
	require.Len(t, operation.Params, 1)
	require.Len(t, operation.Responses, 1)
	require.Len(t, operation.Responses[0].Headers, 1)

	assert.Nil(t, operation.Params[0].Constraints)
	listed, ok := doc.Types[operation.Params[0].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, listed.Constraints)
	assert.Nil(t, operation.Responses[0].Headers[0].Constraints)
	limited, ok := doc.Types[operation.Responses[0].Headers[0].Type.Target].(*ir.List)
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
	operation := openapitest.FirstOperation(t, svc)
	require.Len(t, operation.Params, 2)

	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, operation.Params[0].Constraints)
	assert.Empty(t, operation.Params[0].Unmodeled, "a bound the parameter holds is not also kept")
	assert.Nil(t, operation.Params[1].Constraints, "the List holds its own bound")
	plain, ok := doc.Types[operation.Params[1].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, plain.Constraints)

	require.Len(t, operation.Responses, 1)
	headers := operation.Responses[0].Headers
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
	operation := openapitest.FirstOperation(t, svc)
	require.Len(t, operation.Params, 1)
	constraints := operation.Params[0].Constraints
	require.NotNil(t, constraints, "param scalar constraints land via annotation.Constraints")
	require.NotNil(t, constraints.Max)
	assert.Equal(t, ir.BigVal("120"), *constraints.Max, "numeric bound read at full precision")
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

	parameter := openapitest.FirstOperation(t, svc).Params[0]
	openapitest.AssertProbeDocsKept(t, parameter.Docs)
	assert.NotNil(t, parameter.Deprecation)
	openapitest.AssertProbeExample(t, parameter.Examples)
	assert.Contains(t, parameter.Unmodeled, "openapi:schema/x-vendor")
	assert.Contains(t, parameter.Unmodeled, "openapi:schema/not")
	require.NotNil(t, parameter.Constraints)
	require.NotNil(t, parameter.Constraints.MaxLength)
	assert.Equal(t, int64(3), *parameter.Constraints.MaxLength)
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

	parameter := openapitest.FirstOperation(t, svc).Params[0]
	assert.Equal(t, ir.TypeID("type/primitive/string"), parameter.Type.Target,
		"the parameter's own type is unchanged by the outside reference")
	openapitest.AssertProbeDocsKept(t, parameter.Docs)
	assert.NotNil(t, parameter.Deprecation)
	assert.Contains(t, parameter.Unmodeled, "openapi:schema/x-vendor")
	assert.Contains(t, parameter.Unmodeled, "openapi:schema/not")

	sc, ok := doc.Types["type/anonymous/paths/~1x/get/parameters/0/schema"].(*ir.Scalar)
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
	return openapitest.IndexBy(openapitest.FirstOperation(t, svc).Params, func(parameter ir.Parameter) string { return parameter.Name.Source })
}

// TestParams_RefSchemaInheritsFromItsReferent covers the referent-fallback half
// of the $ref rule at a parameter (GitHub #131). A parameter whose schema is a
// bare $ref used to carry nothing at all — no description, no deprecation, no
// default — because the position never resolved the target it was holding.
func TestParams_RefSchemaInheritsFromItsReferent(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	parameter := paramsOf(t, svc)["bare"]
	assert.Equal(t, "REFERENT", parameter.Docs.Description, "the referent's description reaches the parameter")
	assert.NotNil(t, parameter.Deprecation, "and its deprecation")
	require.NotNil(t, parameter.Default, "and its default")
	assert.Equal(t, "referent", parameter.Default.String)
	assert.Nil(t, parameter.Constraints,
		"but never its constraints: fillPropertyConstraints keeps those use-site-only too")
}

// TestParams_RefSchemaUseSiteWinsOverItsReferent pins the half that already
// worked, so closing the fallback cannot quietly invert the precedence: what is
// written beside the $ref describes this use of the type and outranks the
// target, bar a deprecated false, which JSON Schema reads as absent.
func TestParams_RefSchemaUseSiteWinsOverItsReferent(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	parameter := paramsOf(t, svc)["override"]
	assert.Equal(t, "USE_SITE", parameter.Docs.Description, "the use-site description wins")
	require.NotNil(t, parameter.Default)
	assert.Equal(t, "use-site", parameter.Default.String, "and the use-site default")
	assert.NotNil(t, parameter.Deprecation, "but a deprecated: false beside the $ref withdraws no referent's true")
}

// TestParams_RefSchemaInheritsThroughARefChain pins which resolution the
// fallback must use. Hop writes nothing but its own $ref, so a one-hop referent
// (annotation.At's) would leave the parameter with nothing; only following the
// chain to its end reaches Q.
func TestParams_RefSchemaInheritsThroughARefChain(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRefInheritSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	parameter := paramsOf(t, svc)["chained"]
	assert.Equal(t, "REFERENT", parameter.Docs.Description, "one hop reaches Hop, which declares nothing")
	assert.NotNil(t, parameter.Deprecation)
	require.NotNil(t, parameter.Default)
	assert.Equal(t, "referent", parameter.Default.String)
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
		param, schemaPtr, scope, want string
	}{
		{"q", "/paths/~1x/get/parameters/0/schema", "schema", `{"name":"XQ"}`},
		{"c", "/paths/~1x/get/parameters/1/content/application~1xml/schema", "content/application~1xml/schema",
			`{"name":"XCS"}`},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			entry, ok := params[tc.param].Unmodeled["openapi:"+tc.scope+"/xml"]
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

	parameter := paramsOf(t, svc)["obj"]
	node, ok := doc.Types[parameter.Type.Target]
	require.True(t, ok, "the object schema owns a node of its own")
	require.NotNil(t, node.Common().XML)
	assert.Equal(t, "XOBJ", node.Common().XML.Name)
	assert.NotContains(t, parameter.Unmodeled, "openapi:schema/xml", "so the parameter keeps no second copy")
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

// TestParams_SchemaRedactionReachesTheParameter covers the positions where a
// parameter is the carrier: a `format: password` its schema writes owns no node,
// so ir.Parameter.Secret holds it, inline, beside a $ref and in a content-style
// parameter alike, and nothing is kept or announced beside the field (GitHub
// #578). A $ref's referent lends it as it lends a property its own.
func TestParams_SchemaRedactionReachesTheParameter(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramRedactionSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	for _, name := range []string{"q", "c", "beside", "ref"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parameter, ok := params[name]
			require.True(t, ok)
			assert.True(t, parameter.Secret, "the parameter is the carrier")
			assert.NotContains(t, parameter.Unmodeled, "openapi:schema/format", "the field holds it, so nothing is kept beside it")
		})
	}
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"a redaction with a home degrades nothing")
}

// TestParams_SchemaRedactionStaysOnANode is the other half: a parameter whose
// own schema owns a node leaves the redaction there, one home per declaration,
// and one writing a format that names a shape redacts nothing.
func TestParams_SchemaRedactionStaysOnANode(t *testing.T) {
	t.Parallel()
	doc, svc, diags := compileService(t, paramRedactionSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	id, ok := params["id"]
	require.True(t, ok)
	assert.NotContains(t, id.Unmodeled, "openapi:schema/format", "a format naming a shape is no redaction")
	assert.False(t, id.Secret, "a format naming a shape is no redaction")

	pick, ok := params["pick"]
	require.True(t, ok)
	node, ok := doc.Types[pick.Type.Target]
	require.True(t, ok, "its type is a node")
	assert.True(t, node.Common().Sensitive, "the node holds the redaction")
	assert.False(t, pick.Secret, "so the parameter takes no copy")
	assert.NotContains(t, pick.Unmodeled, "openapi:schema/format", "and keeps none")
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

	cases := []struct{ param, keyword, schemaPtr, scope string }{
		{"ro", "readOnly", "/paths/~1x/get/parameters/0/schema", "schema"},
		{"wo", "writeOnly", "/paths/~1x/get/parameters/1/schema", "schema"},
		{"c", "readOnly", "/paths/~1x/get/parameters/2/content/application~1xml/schema", "content/application~1xml/schema"},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			t.Parallel()
			at := tc.schemaPtr + "/" + tc.keyword
			entry, ok := params[tc.param].Unmodeled["openapi:"+tc.scope+"/"+tc.keyword]
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
components:
  schemas:
    G1: {$dynamicAnchor: ghost}
    G2: {$dynamicAnchor: ghost}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	params := paramsOf(t, svc)

	require.Contains(t, params["enc"].Unmodeled, "openapi:schema/contentEncoding")
	assert.Equal(t, ir.ReasonNoIRHome, params["enc"].Unmodeled["openapi:schema/contentEncoding"].Reason)
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/paths/~1x/get/parameters/0/schema/contentEncoding"), "content-vocabulary")

	require.Contains(t, params["dyn"].Unmodeled, "openapi:schema/$dynamicRef")
	assert.Equal(t, ir.ReasonDegradedLowering, params["dyn"].Unmodeled["openapi:schema/$dynamicRef"].Reason)
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

	parameter := paramsOf(t, svc)["ro"]
	require.NotNil(t, parameter.Default, "the default lands in its own field")
	assert.Equal(t, "dq", parameter.Default.String)
	assert.NotContains(t, parameter.Unmodeled, "openapi:schema/default", "and is not restated verbatim beside it")
	for _, diagnostic := range diags {
		assert.NotEqual(t, jsontext.Pointer("/paths/~1x/get/parameters/0/schema/default"), diagnostic.Provenance.Pointer,
			"nor announced as homeless; got %+v", diagnostic)
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

	parameter := openapitest.FirstOperation(t, svc).Params[0]
	assert.Equal(t, "PARAM", parameter.Docs.Description, "the parameter's own description wins")
	raw, ok := parameter.Unmodeled["openapi:x-scope"]
	require.True(t, ok)
	assert.JSONEq(t, `"param"`, string(raw.Value), "and its own extension overlays the schema's")
}

// TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode pins the half of the
// parameter-visibility rescue that the own-node guard used to swallow. A
// parameter whose schema is an object, an enum or an array hoists a node, and
// neither home held the keywords: the residue a declaration keeps skips the
// position because a parameter is an annotation.HomeCarrier, and ir.Parameter
// has no Visibility field, so they were dropped for exactly the shapes that own
// a node.
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
	operation := openapitest.FindOperation(t, doc, "")
	require.Len(t, operation.Params, 3)

	for i, want := range []string{"openapi:schema/readOnly", "openapi:schema/writeOnly", "openapi:schema/readOnly"} {
		param := operation.Params[i]
		entry, ok := param.Unmodeled[want]
		require.True(t, ok, "%s: %s kept on the carrier that has no field for it", param.Name.Source, want)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		openapitest.AssertInfoDiagAt(t, diags, string(entry.Provenance.Pointer))
	}
}

// TestParams_RefSiteKeywordsAreKeptOnTheParameter covers the third
// annotation.HomeCarrier position keeping a $ref's unread siblings (GitHub #283).
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

	params := openapitest.IndexBy(openapitest.FirstOperation(t, svc).Params, func(parameter ir.Parameter) string { return parameter.Name.Source })
	irParameter, ok := params["q"]
	require.True(t, ok)
	entry, ok := irParameter.Unmodeled["openapi:schema/format"]
	require.True(t, ok, "an alias over the target has no Encoding field, so the format is kept")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `"email"`, string(entry.Value))
	openapitest.AssertInfoDiagAt(t, diags, "/paths/~1x/get/parameters/0/schema")

	parameterValue, ok := params["r"]
	require.True(t, ok)
	assert.Empty(t, parameterValue.Unmodeled, "a bound at the same position always reached ir.Parameter.Constraints")
	require.NotNil(t, parameterValue.Constraints)
	assert.Equal(t, int64(3), *parameterValue.Constraints.MinLength)
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
			constraints := params[tc.param].Constraints
			require.NotNil(t, constraints, "both bounds reach the parameter's constraints")
			require.NotNil(t, constraints.Min)
			require.NotNil(t, constraints.ExclusiveMin)
			assert.Equal(t, tc.wantMin, constraints.Min.String(), "minimum as written")
			assert.Equal(t, tc.wantExclMin, constraints.ExclusiveMin.String(), "exclusiveMinimum as written, beside it")
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

	entry, ok := params["bare"].Unmodeled["openapi:schema/exclusiveMinimum"]
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

	withSiblings, ok := openapitest.ParamBySource(openapitest.FindOperation(t, doc, "a"), "limit")
	require.True(t, ok)
	assert.Equal(t, "S", withSiblings.Docs.Summary)
	assert.Equal(t, "D", withSiblings.Docs.Description)

	without, ok := openapitest.ParamBySource(openapitest.FindOperation(t, doc, "b"), "limit")
	require.True(t, ok)
	assert.Empty(t, without.Docs.Summary, "no sibling means no summary is invented")
	assert.Equal(t, "declared", without.Docs.Description,
		"the other mount keeps the declaration's own description")
}

// TestParameter_AnEntrySpeaksAsItsSchemaDoes pins a parameter's schema refining a
// $ref: what its entries write lands as what the schema writes does, a redaction
// on Secret, and what ir.Parameter has no field for is kept verbatim at the
// keyword that wrote it.
func TestParameter_AnEntrySpeaksAsItsSchemaDoes(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    get:
      operationId: get
      parameters:
        - name: q
          in: query
          schema:
            allOf:
              - {$ref: '#/components/schemas/Str'}
              - {description: mine, maxLength: 3, readOnly: true, format: password, xml: {name: q}}
      responses: {"200": {description: ok}}
components:
  schemas:
    Str: {type: string, title: A string}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	param, ok := openapitest.ParamBySource(openapitest.FindOperation(t, doc, "get"), "q")
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("type/openapi/components/schemas/Str"), param.Type.Target)
	assert.Equal(t, "mine", param.Docs.Description)
	assert.Equal(t, "A string", param.Docs.Summary)
	require.NotNil(t, param.Constraints)
	assert.Equal(t, new(int64(3)), param.Constraints.MaxLength)
	assert.True(t, param.Secret, "an entry's format: password redacts the parameter")
	assert.NotContains(t, param.Unmodeled, "openapi:schema/format", "which then needs no verbatim copy")
	for _, key := range []string{"openapi:schema/readOnly", "openapi:schema/xml"} {
		entry, kept := param.Unmodeled[key]
		if assert.True(t, kept, "%s: got %v", key, param.Unmodeled) {
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason, key)
			assert.Contains(t, string(entry.Provenance.Pointer), "/allOf/1/", key)
		}
	}
}

// TestParameter_APrincipalOwningANodeKeepsItsVisibility pins readOnly and writeOnly
// on the entry a refinement lowers from: a node holds neither, so the parameter
// keeps them as it does written on its schema, at the keyword that wrote them.
func TestParameter_APrincipalOwningANodeKeepsItsVisibility(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    get:
      operationId: get
      parameters:
        - name: list
          in: query
          schema: {allOf: [{type: array, items: {type: string}, readOnly: true}]}
        - name: narrowed
          in: query
          schema: {allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [a], writeOnly: true}]}
      responses: {"200": {description: ok}}
components:
  schemas:
    Mode: {type: string, enum: [a, b]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for source, key := range map[string]string{"list": "openapi:schema/readOnly", "narrowed": "openapi:schema/writeOnly"} {
		param, ok := openapitest.ParamBySource(openapitest.FindOperation(t, doc, "get"), source)
		require.True(t, ok, source)
		entry, kept := param.Unmodeled[key]
		if assert.True(t, kept, "%s: got %v", source, param.Unmodeled) {
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason, source)
			assert.Contains(t, string(entry.Provenance.Pointer), "/allOf/", source)
		}
	}
}

// TestParameter_AnEntrySeveralWriteIsTheNearest pins what a parameter keeps of one
// keyword written twice, as a node's residue does: the schema written at the
// position over its entries, and the first entry over a later one.
func TestParameter_AnEntrySeveralWriteIsTheNearest(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    get:
      operationId: get
      parameters:
        - name: beside
          in: query
          schema:
            readOnly: true
            xml: {name: a}
            allOf: [{$ref: '#/components/schemas/Str'}, {readOnly: true, xml: {name: a}}]
        - name: branches
          in: query
          schema:
            allOf: [{$ref: '#/components/schemas/Str'}, {writeOnly: true, xml: {name: b}}, {writeOnly: true, xml: {name: b}}]
      responses: {"200": {description: ok}}
components:
  schemas:
    Str: {type: string}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	base := func(i int) string { return "/paths/~1p/get/parameters/" + strconv.Itoa(i) + "/schema" }
	for _, tc := range []struct {
		source, key string
		at          string
	}{
		{"beside", "openapi:schema/readOnly", base(0) + "/readOnly"},
		{"beside", "openapi:schema/xml", base(0) + "/xml"},
		{"branches", "openapi:schema/writeOnly", base(1) + "/allOf/1/writeOnly"},
		{"branches", "openapi:schema/xml", base(1) + "/allOf/1/xml"},
	} {
		param, ok := openapitest.ParamBySource(openapitest.FindOperation(t, doc, "get"), tc.source)
		require.True(t, ok, tc.source)
		entry, kept := param.Unmodeled[tc.key]
		require.True(t, kept, tc.key)
		assert.Equal(t, tc.at, string(entry.Provenance.Pointer), tc.source)
	}
}

// TestParameter_ADefaultReadsOnlyWhereTheNarrowingTakesIt pins a parameter's default
// across a value set narrowing its $ref, as a property's reads.
func TestParameter_ADefaultReadsOnlyWhereTheNarrowingTakesIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /p:
    get:
      operationId: get
      parameters:
        - {name: taken, in: query, schema: {allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [a]}]}}
        - {name: dropped, in: query, schema: {allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [b]}]}}
      responses: {"200": {description: ok}}
components:
  schemas:
    Mode: {type: string, enum: [a, b], default: a}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	operation := openapitest.FindOperation(t, doc, "get")
	taken, ok := openapitest.ParamBySource(operation, "taken")
	require.True(t, ok)
	assert.NotNil(t, taken.Default)
	dropped, ok := openapitest.ParamBySource(operation, "dropped")
	require.True(t, ok)
	assert.Nil(t, dropped.Default)
}

// paramBoundsSpec writes a bound beside a reference at each carrier a schema
// can sit at: three parameters and a response header.
const paramBoundsSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {name: q, in: query, schema: {allOf: [{$ref: '#/components/schemas/Name'}], minimum: 1}}
        - {name: r, in: query, schema: {$ref: '#/components/schemas/Name', minimum: 1}}
        - {name: h, in: header, schema: {allOf: [{$ref: '#/components/schemas/Name'}], minimum: 1, minLength: 2}}
      responses:
        "200":
          description: ok
          headers:
            X-Names: {schema: {$ref: '#/components/schemas/Names', maxLength: 5, maxItems: 3}}
components:
  schemas:
    Name: {type: string}
    Names: {type: array, items: {type: string}}
`

// TestParams_ABoundBesideAReferenceIsReadByItsNamedType pins a bound beside a
// reference at a parameter and at a response header: it is held by the carrier
// only where the type the schema names takes it, and kept verbatim, with its
// report, where it does not.
func TestParams_ABoundBesideAReferenceIsReadByItsNamedType(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, paramBoundsSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	operation := openapitest.FirstOperation(t, svc)

	t.Run("row 38: a bound a string cannot take is kept", func(t *testing.T) {
		t.Parallel()
		for i, name := range []string{"q", "r"} {
			parameter, ok := openapitest.ParamBySource(operation, name)
			require.True(t, ok, name)
			assert.Equal(t, ir.ParameterID("parameter/openapi/paths/~1a/get/parameters/"+name+"/query"), parameter.ID, name)
			assert.Nil(t, parameter.Constraints, name)
			entry, ok := parameter.Unmodeled["openapi:schema/minimum"]
			require.True(t, ok, "%s: got %v", name, parameter.Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, name)
			at := "/paths/~1a/get/parameters/" + strconv.Itoa(i) + "/schema"
			assert.Equal(t, jsontext.Pointer(at+"/minimum"), entry.Provenance.Pointer, name)
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at),
				"minimum", name)
		}
	})
	t.Run("row 39: the bounds a type takes are held beside the kept one", func(t *testing.T) {
		t.Parallel()
		parameter, ok := openapitest.ParamBySource(operation, "h")
		require.True(t, ok)
		assert.Equal(t, &ir.Constraints{MinLength: new(int64(2))}, parameter.Constraints)
		assert.Contains(t, parameter.Unmodeled, "openapi:schema/minimum")
		assert.NotContains(t, parameter.Unmodeled, "openapi:schema/minLength")

		require.Len(t, operation.Responses, 1)
		require.Len(t, operation.Responses[0].Headers, 1)
		header := operation.Responses[0].Headers[0]
		assert.Equal(t, ir.PropertyID("property/openapi/paths/~1a/get/responses/200/headers/X-Names"), header.ID)
		assert.Equal(t, &ir.Constraints{MaxItems: new(int64(3))}, header.Constraints)
		entry, ok := header.Unmodeled["openapi:schema/maxLength"]
		require.True(t, ok, "got %v", header.Unmodeled)
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/responses/200/headers/X-Names/schema/maxLength"),
			entry.Provenance.Pointer)
		assert.NotContains(t, header.Unmodeled, "openapi:schema/maxItems")
	})
}

// TestParams_AnUnconvertibleDefaultIsReportedWhereItIsWritten holds a
// parameter's default to the reading a property's takes (schema.Use.Default):
// one the IR holds no value for leaves the parameter none and is reported once,
// as a construct left unlowered, at the default itself rather than its schema.
func TestParams_AnUnconvertibleDefaultIsReportedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(`  /x:
    get:
      parameters:
        - {name: n, in: query, schema: {type: number, default: .inf}}
      responses: {"200": {description: ok}}
`))
	assert.Nil(t, paramsOf(t, svc)["n"].Default)
	var found []string
	for _, diagnostic := range diags {
		if strings.Contains(diagnostic.Message, "default") {
			found = append(found, diagnostic.Code+" "+string(diagnostic.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"openapi/unlowered-construct /paths/~1x/get/parameters/0/schema/default"}, found)
}

// TestParams_NotesAreTheParametersWhateverItsSchemaHoists pins a parameter as
// the home of what its schema notes where that schema hoists a node, as where
// it hoists none: written inline, as content and in a component, each schema
// a list, an enum or a model, the parameter holds the deprecation and a
// promoted member with its mark, and the documentation and examples unless it
// writes its own, the node keeping the schema's then, so none is lost
// (docs/ir/annotations.md#one-structural-home-per-declaration), and a plain
// use of a component one use describes reads them on the node. Both orders of
// the components compile alike.
func TestParams_NotesAreTheParametersWhateverItsSchemaHoists(t *testing.T) {
	t.Parallel()
	const notes = "description: DOC, deprecated: true, x-deprecated-reason: why, examples: [[a]], format: password"
	const list = "{type: array, items: {type: string}, " + notes + "}"
	components := []string{
		"Shared: {name: shared, in: query, schema: {type: object, properties: {a: {type: string}}, " + notes + "}}",
		"Described: {name: described, in: query, description: OWN, schema: " + list + "}",
		"Used: {name: used, in: query, schema: " + list + "}",
		"Aliased: {$ref: '#/components/parameters/Hopped', description: HOP}",
		"Hopped: {name: hopped, in: query, schema: " + list + "}",
	}
	compile := func(components []string) (*ir.Document, ir.Service) {
		doc, svc, diags := compileService(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"+
			"  /x:\n    get:\n      operationId: g\n      parameters:\n"+
			"        - {name: inline, in: query, schema: "+list+"}\n"+
			"        - {name: content, in: query, content: {application/json: {schema: "+list+"}}}\n"+
			"        - {name: own, in: query, description: OWN, schema: "+list+"}\n"+
			"        - {name: en, in: query, description: OWN, schema: {type: string, enum: [a, b], "+
			"description: DOC, deprecated: true, x-deprecated-reason: why, examples: [[a]]}}\n"+
			"        - {name: example, in: query, example: [b], schema: "+list+"}\n"+
			"        - {name: entry, in: query, content: {application/json: {examples: {e: {value: [b]}}, schema: "+list+"}}}\n"+
			"        - $ref: '#/components/parameters/Shared'\n"+
			"        - $ref: '#/components/parameters/Described'\n"+
			"        - {$ref: '#/components/parameters/Used', description: USE}\n"+
			"        - $ref: '#/components/parameters/Aliased'\n"+
			"      responses: {\"204\": {description: none}}\n"+
			"  /y:\n    get:\n      operationId: h\n      parameters:\n"+
			"        - $ref: '#/components/parameters/Used'\n"+
			"      responses: {\"204\": {description: none}}\n"+
			"components:\n  parameters:\n    "+strings.Join(components, "\n    ")+"\n")
		openapitest.RequireNoErrorDiags(t, diags)
		return doc, svc
	}
	doc, svc := compile(components)
	operation := openapitest.FirstOperation(t, svc)
	for _, tc := range []struct {
		name, docs, nodeDocs   string
		examples, nodeExamples []string
	}{
		{"inline", "DOC", "", []string{"a"}, nil},
		{"content", "DOC", "", []string{"a"}, nil},
		{"own", "OWN", "DOC", []string{"a"}, nil},
		{"en", "OWN", "DOC", []string{"a"}, nil},
		{"example", "DOC", "", []string{"b"}, []string{"a"}},
		{"entry", "DOC", "", []string{"b"}, []string{"a"}},
		{"shared", "DOC", "", []string{"a"}, nil},
		{"described", "OWN", "DOC", []string{"a"}, nil},
		{"used", "USE", "DOC", []string{"a"}, nil},
		{"hopped", "HOP", "DOC", []string{"a"}, nil},
	} {
		param, ok := openapitest.ParamBySource(operation, tc.name)
		require.True(t, ok, tc.name)
		assert.Equal(t, tc.docs, param.Docs.Description, tc.name)
		assert.Equal(t, tc.examples, listedExamples(param.Examples), tc.name)
		assert.Equal(t, &ir.Deprecation{Message: "why"}, param.Deprecation, tc.name)
		assert.Equal(t, ir.Inferred("extension-promotion"), param.Provenance.Inferred, tc.name)
		assert.False(t, param.Secret, "%s: the node keeps the redaction", tc.name)

		node, ok := doc.Types[param.Type.Target]
		require.True(t, ok, "%s: the schema hoists a node", tc.name)
		require.True(t, node.Common().Anonymous, tc.name)
		assert.Equal(t, tc.nodeDocs, node.Common().Docs.Description, tc.name)
		assert.Equal(t, tc.nodeExamples, listedExamples(node.Common().Examples), tc.name)
		assert.Nil(t, node.Common().Deprecation, tc.name)
		assert.Equal(t, tc.name != "en", node.Common().Sensitive, tc.name)
		assert.Contains(t, node.Common().Unmodeled, "openapi:x-deprecated-reason", tc.name)
	}

	described, ok := openapitest.ParamBySource(operation, "used")
	require.True(t, ok)
	plain, ok := openapitest.ParamBySource(openapitest.FindOperation(t, doc, "h"), "used")
	require.True(t, ok)
	assert.Empty(t, plain.Docs.Description, "a plain use reads the docs on the node")
	assert.Equal(t, described.Examples, plain.Examples)
	assert.Equal(t, described.Deprecation, plain.Deprecation)
	assert.Equal(t, described.Type.Target, plain.Type.Target, "both uses name one node")
	node, ok := doc.Types[plain.Type.Target]
	require.True(t, ok)
	assert.Equal(t, "DOC", node.Common().Docs.Description)

	reversed := slices.Clone(components)
	slices.Reverse(reversed)
	reversedDoc, reversedSvc := compile(reversed)
	assert.Empty(t, cmp.Diff(doc.Types, reversedDoc.Types), "the types do not depend on component order")
	assert.Empty(t, cmp.Diff(svc, reversedSvc), "nor do the parameters")
}

// listedExamples returns the strings each of examples lists, or the string it
// is, in order, nil where there are none.
func listedExamples(examples []ir.Example) []string {
	var listed []string
	for _, example := range examples {
		if example.Value == nil {
			continue
		}
		if example.Value.Kind == ir.ValueString {
			listed = append(listed, example.Value.String)
		}
		for _, item := range example.Value.List {
			listed = append(listed, item.String)
		}
	}
	return listed
}
