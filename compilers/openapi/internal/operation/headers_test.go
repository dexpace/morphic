// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestContent_RefdResponseHeaderNestedRefInternsSchemaOnce covers the nested
// hop of issue #107: a $ref'd response whose own headers entry is itself a
// $ref to a header component must still hoist the header's schema once, at
// the header component's own declaration pointer.
func TestContent_RefdResponseHeaderNestedRefInternsSchemaOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdResponseNestedHeaderSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	operation := openapitest.FindOperation(t, doc, "getA")
	require.Len(t, operation.Responses, 1)
	require.Len(t, operation.Responses[0].Headers, 1)

	wantID := ir.TypeID("type/anonymous/components/headers/Rate/schema")
	assert.Equal(t, wantID, operation.Responses[0].Headers[0].Type.Target)
	_, ok := doc.Types[wantID]
	require.True(t, ok, "the header's schema is registered under the header component's own pointer")
}

// TestContent_HeaderMapEntriesSharingComponentGetDistinctIDs covers the
// header-identity half of issue #107: two response headers under different
// map keys that both $ref the same header component keep distinct PropIDs —
// the map entry, not the schema, identifies each header — while sharing one
// Type.Target.
func TestContent_HeaderMapEntriesSharingComponentGetDistinctIDs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, headerIdentitySpec)
	openapitest.RequireNoErrorDiags(t, diags)
	operation := openapitest.FindOperation(t, doc, "getA")
	require.Len(t, operation.Responses[0].Headers, 2)
	byWire := openapitest.IndexBy(operation.Responses[0].Headers, func(header ir.Header) string { return header.WireName })

	rate, limit := byWire["X-Rate"], byWire["X-Limit"]
	assert.NotEqual(t, rate.ID, limit.ID, "distinct map keys keep distinct PropIDs")
	assert.Equal(t, ir.PropertyID("property/openapi/paths/~1a/get/responses/200/headers/X-Rate"), rate.ID)
	assert.Equal(t, ir.PropertyID("property/openapi/paths/~1a/get/responses/200/headers/X-Limit"), limit.ID)
	assert.Equal(t, rate.Type.Target, limit.Type.Target, "both resolve the same shared header schema")
	assert.Equal(t, ir.TypeID("type/anonymous/components/headers/Rate/schema"), rate.Type.Target)
}

// TestContent_EncodingHeaderRefInternsAtDeclaration covers lowerHeaders' other
// caller: a multipart part's per-encoding headers. A $ref'd header there must
// split the same way a response header does — schema at the component, identity
// (PropertyID, provenance) at the encoding entry that binds the name (issue #107).
func TestContent_EncodingHeaderRefInternsAtDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdEncodingHeaderSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	operation := openapitest.FindOperation(t, doc, "upload")
	require.NotNil(t, operation.Request)
	require.Len(t, operation.Request.Contents, 1)
	enc := operation.Request.Contents[0].Encoding
	require.Len(t, enc, 1)

	var headers []ir.Header
	for _, pe := range enc {
		headers = pe.Headers
	}
	require.Len(t, headers, 1)
	assert.Equal(t, ir.TypeID("type/anonymous/components/headers/Rate/schema"), headers[0].Type.Target,
		"the encoding header's schema interns at the header component")
	assert.Equal(t,
		ir.PropertyID("property/openapi/paths/~1u/post/requestBody/content/multipart~1form-data/encoding/file/headers/X-Rate"),
		headers[0].ID, "the encoding entry that binds the name keeps the header's identity")
	assert.Equal(t, headers[0].Provenance.Pointer, jsontext.Pointer(string(headers[0].ID)[len("property/openapi"):]),
		"provenance tracks the same use-site pointer as the ID")
}

// TestHeaders_SchemaDetailReachesTheProperty asserts a header's schema is read
// with the same detail a model property's is. lowerHeaders built an ir.Property
// and never filled it, so a header schema dropped its docs, xml, extensions,
// validation-only keywords and value constraints even though ir.Property has a
// field for each (GitHub #116).
func TestHeaders_SchemaDetailReachesTheProperty(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H: {schema: "+openapitest.InlineProbeBody+"}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	header := openapitest.FirstOperation(t, svc).Responses[0].Headers[0]
	openapitest.AssertProbeDocsKept(t, header.Docs)
	assert.NotNil(t, header.Deprecation)
	openapitest.AssertProbeExample(t, header.Examples)
	require.NotNil(t, header.XML)
	assert.Equal(t, "X", header.XML.Name)
	assert.Contains(t, header.Unmodeled, "openapi:schema/x-vendor")
	assert.Contains(t, header.Unmodeled, "openapi:schema/not")
	require.NotNil(t, header.Constraints)
	require.NotNil(t, header.Constraints.MaxLength)
	assert.Equal(t, int64(3), *header.Constraints.MaxLength)
}

// TestHeaders_OwnAnnotationsOverrideTheSchema checks the precedence between the
// two annotation sources a header has: what the header object writes about
// itself is more specific than what its schema writes about the type, so it
// wins where both are set.
//
// Every annotation applyHeaderAnnotations can override is written on both sides
// here, with a value naming the side it came from. A keyword only one side
// declares proves nothing about precedence: it reaches the header in either
// overlay order, so the assertion over it passes whichever side wins.
func TestHeaders_OwnAnnotationsOverrideTheSchema(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H:\n              description: HEADER\n              example: HEADER\n"+
			"              x-scope: header\n              schema:\n"+
			"                {type: string, description: SCHEMA, example: SCHEMA, x-scope: schema}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	header := openapitest.FirstOperation(t, svc).Responses[0].Headers[0]
	assert.Equal(t, "HEADER", header.Docs.Description, "the header's own description wins")
	require.Len(t, header.Examples, 1, "and its own example replaces the schema's rather than joining it")
	require.NotNil(t, header.Examples[0].Value)
	assert.Equal(t, "HEADER", header.Examples[0].Value.String)
	require.Contains(t, header.Unmodeled, "openapi:x-scope")
	assert.JSONEq(t, `"header"`, string(header.Unmodeled["openapi:x-scope"].Value),
		"and its own value for a vendor key the schema also writes")
}

// TestHeaders_DeprecationUnionsWithTheSchema pins the one annotation a header
// does not override. Both overlays only ever set the flag, so `deprecated`
// written on either side deprecates the header and neither side can clear the
// other: a header's own `deprecated: false` does not un-deprecate a type its
// schema marks deprecated. That is a union rather than the precedence the test
// above covers, which is why deprecation is asserted here and not there.
func TestHeaders_DeprecationUnionsWithTheSchema(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, header, schema string }{
		{"declared on the header, denied by the schema", "true", "false"},
		{"declared on the schema, denied by the header", "false", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, openapitest.PathsSpec(
				"  /x:\n    get:\n      operationId: g\n      responses:\n"+
					"        \"200\":\n          description: ok\n          headers:\n"+
					"            X-H:\n              deprecated: "+tc.header+"\n"+
					"              schema: {type: string, deprecated: "+tc.schema+"}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			header := openapitest.FirstOperation(t, svc).Responses[0].Headers[0]
			assert.NotNil(t, header.Deprecation, "either side alone deprecates the header")
		})
	}
}

// TestHeaders_SerializationKeywordsLower covers the two keywords a header object
// writes about its serialization, which lower onto ir.Header's Style and
// Explode. explode decides how an object-valued header is joined on the wire,
// so dropping it left the IR unable to say how the header serializes — and it
// was once dropped in silence, at both of lowerHeaders' callers.
//
// Both callers are exercised, because the reading belongs to the shared
// lowering rather than to the response position it was noticed at: a multipart
// part's per-encoding headers build the same ir.Header from the same function.
func TestHeaders_SerializationKeywordsLower(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, spec, at string }{
		{
			name: "response header",
			spec: openapitest.PathsSpec("  /x:\n    get:\n      operationId: g\n      responses:\n" +
				"        \"200\":\n          description: ok\n          headers:\n" +
				"            X-H:\n              style: simple\n              explode: true\n" +
				"              schema: {type: array, items: {type: string}}\n"),
			at: "/paths/~1x/get/responses/200/headers/X-H",
		},
		{
			name: "multipart encoding header",
			spec: openapitest.PathsSpec("  /x:\n    post:\n      operationId: g\n      requestBody:\n" +
				"        content:\n          multipart/form-data:\n" +
				"            schema: {type: object, properties: {file: {type: string}}}\n" +
				"            encoding:\n              file:\n                headers:\n" +
				"                  X-H:\n                    style: simple\n" +
				"                    explode: true\n" +
				"                    schema: {type: array, items: {type: string}}\n" +
				"      responses: {\"200\": {description: ok}}\n"),
			at: "/paths/~1x/post/requestBody/content/multipart~1form-data/encoding/file/headers/X-H",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, tc.spec)
			openapitest.RequireNoErrorDiags(t, diags)
			assertHeaderSerialization(t, headerAt(t, openapitest.FirstOperation(t, svc)), diags, tc.at)
		})
	}
}

// TestHeaders_SerializationKeywordsAbsentRecordNothing is the control for the
// test above: the fields hold what the header declares, so a header declaring
// neither keyword sets neither — rather than writing the OpenAPI defaults the
// accessors would hand back and calling them source facts.
func TestHeaders_SerializationKeywordsAbsentRecordNothing(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H: {schema: {type: string}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	header := openapitest.FirstOperation(t, svc).Responses[0].Headers[0]
	assert.Empty(t, header.Style, "an undeclared style is not written as the default")
	assert.Nil(t, header.Explode, "nor an undeclared explode")
	assert.Empty(t, header.Unmodeled)
	assert.Empty(t, diags, "and nothing is announced about keywords the header never wrote")
}

// TestHeaders_SerializationKeywordsFollowTheDeclaration pins whose keywords a
// header entry written as a Reference Object reads: the component's, as its
// schema is, and a declared explode: false stays a declaration rather than
// collapsing into the unset default.
func TestHeaders_SerializationKeywordsFollowTheDeclaration(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: g
      responses:
        "200":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/Joined'}
components:
  headers:
    Joined:
      style: simple
      explode: false
      schema: {type: object, properties: {r: {type: integer}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	header := openapitest.FirstOperation(t, svc).Responses[0].Headers[0]
	assert.Equal(t, "simple", header.Style)
	require.NotNil(t, header.Explode, "a declared false is a declaration")
	assert.False(t, *header.Explode)
}

// TestHeaders_ReservedContentTypeEntryIsReported covers the response side of
// the headers-map half of the rule diag.ReservedHeaderName records: a
// Content-Type entry (§4.8.17), in any casing, is reported at the entry's own
// pointer, and other names, Accept included, are not. The header lowers either
// way, because dropping declared content is a loss and the choice belongs to an
// emitter; the diagnostic is what lets an emitter tell it from any other
// header. The encoding side is
// TestHeaders_ReservedContentTypeInEncodingIsReported.
func TestHeaders_ReservedContentTypeEntryIsReported(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, header, at string
		reported         bool
	}{
		{"response Content-Type", "Content-Type",
			"/paths/~1x/get/responses/200/headers/Content-Type", true},
		{"response lowercase spelling", "content-type",
			"/paths/~1x/get/responses/200/headers/content-type", true},
		{"response ordinary header", "X-Trace",
			"/paths/~1x/get/responses/200/headers/X-Trace", false},
		{"response Accept is not reserved here", "Accept",
			"/paths/~1x/get/responses/200/headers/Accept", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, openapitest.PathsSpec(
				"  /x:\n    get:\n      operationId: g\n      responses:\n"+
					"        \"200\":\n          description: ok\n          headers:\n"+
					"            "+tc.header+": {schema: {type: string}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			headers := openapitest.FirstOperation(t, svc).Responses[0].Headers
			require.Len(t, headers, 1, "the header lowers either way; nothing is dropped")
			assert.Equal(t, tc.header, headers[0].WireName)
			assert.Equal(t, tc.reported, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, tc.at),
				"reported at the map entry's own pointer")
		})
	}
}

// TestHeaders_ReservedContentTypeInEncodingIsReported is the same rule at the
// other caller of lowerHeaders: an encoding's headers map, which §4.8.15 says
// describes Content-Type separately and SHALL ignore an entry for it.
func TestHeaders_ReservedContentTypeInEncodingIsReported(t *testing.T) {
	t.Parallel()
	_, _, diags := compileService(t, openapitest.PathsSpec(
		"  /x:\n    post:\n      operationId: g\n      requestBody:\n"+
			"        content:\n          multipart/form-data:\n"+
			"            schema: {type: object, properties: {file: {type: string}}}\n"+
			"            encoding:\n              file:\n                headers:\n"+
			"                  Content-Type: {schema: {type: string}}\n"+
			"                  X-Other: {schema: {type: string}}\n"+
			"      responses: {\"200\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	base := "/paths/~1x/post/requestBody/content/multipart~1form-data/encoding/file/headers/"
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"Content-Type"))
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"X-Other"),
		"the entry beside it is ordinary and says nothing")
	openapitest.AssertHasCode(t, diags, diag.ReservedHeaderName, ir.SeverityWarning)
}

// TestHeaders_ReservedNameIsTheKeyNotTheDeclaration pins which of two pointers
// the report lands on. The reserved thing is the key the header is mapped under,
// not the header object: one component $ref'd from a reserved key and an
// ordinary one is two declarations, and only the reserved key is reported —
// reporting at the shared declaration instead would collapse them into one.
func TestHeaders_ReservedNameIsTheKeyNotTheDeclaration(t *testing.T) {
	t.Parallel()
	_, _, diags := compileService(t, `openapi: 3.1.0
info: {title: T, version: "1"}
components:
  headers:
    Shared: {schema: {type: string}}
paths:
  /x:
    get:
      operationId: g
      responses:
        "200":
          description: ok
          headers:
            Content-Type: {$ref: '#/components/headers/Shared'}
            X-Other: {$ref: '#/components/headers/Shared'}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	base := "/paths/~1x/get/responses/200/headers/"
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"Content-Type"),
		"the reserved key is reported at its own use site")
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"X-Other"),
		"the other key sharing that declaration is not")
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, "/components/headers/Shared"),
		"and nothing is reported at the declaration they share")
}

// TestHeaders_RefSiteKeywordsAreKeptOnTheHeader covers the header position of
// a $ref and the keywords beside it (GitHub #283): one conjunction, as the
// allOf of the two is, so a value set narrowing the target is the header
// schema's own Enum, which keeps the $ref it narrows, and the header keeps
// nothing.
func TestHeaders_RefSiteKeywordsAreKeptOnTheHeader(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x:\n    get:\n      operationId: g\n      responses:\n" +
		"        \"200\":\n          description: ok\n          headers:\n" +
		"            X-H: {schema: {$ref: '#/components/schemas/Base', enum: [a, b]}}\n" +
		"components:\n  schemas:\n    Base: {type: string}\n"
	doc, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	header := headerAt(t, openapitest.FirstOperation(t, svc))
	assert.Empty(t, header.Unmodeled, "the header keeps nothing")
	en, ok := doc.Types[header.Type.Target].(*ir.Enum)
	require.True(t, ok, "the header names the value set, got %T", doc.Types[header.Type.Target])
	entry, ok := en.Unmodeled["openapi:$ref"]
	require.True(t, ok, "the enum keeps the $ref it narrows")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	openapitest.AssertInfoDiagAt(t, diags, "/paths/~1x/get/responses/200/headers/X-H/schema")
}

// TestHeaders_RefSiteDocsOverrideTheDeclaration pins the header half of GitHub
// #610: a header entry written as a Reference Object keeps the pair it writes
// beside the $ref, overriding the declaration's own description.
func TestHeaders_RefSiteDocsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          headers:
            X-Rate:
              $ref: '#/components/headers/Rate'
              summary: HS
              description: HD
components:
  headers:
    Rate: {description: declared, schema: {type: integer}}
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, openapitest.FirstOperation(t, svc).Responses, 1)
	header := openapitest.FirstOperation(t, svc).Responses[0].Headers[0]
	assert.Equal(t, "HS", header.Docs.Summary)
	assert.Equal(t, "HD", header.Docs.Description)
}

const refdResponseNestedHeaderSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/OK'}
components:
  responses:
    OK:
      description: ok
      headers:
        X-Rate: {$ref: '#/components/headers/Rate'}
  headers:
    Rate: {schema: {type: string, enum: [a, b]}}
`

const refdEncodingHeaderSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /u:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
            encoding:
              file:
                headers:
                  X-Rate: {$ref: '#/components/headers/Rate'}
      responses: {"200": {description: ok}}
components:
  headers:
    Rate: {schema: {type: string, enum: [a, b]}}
`

// headerAt returns the one header a serialization fixture declares, whichever
// of the two positions carries it.
func headerAt(t *testing.T, operation ir.Operation) ir.Header {
	t.Helper()
	if len(operation.Responses) > 0 && len(operation.Responses[0].Headers) > 0 {
		return operation.Responses[0].Headers[0]
	}
	require.NotNil(t, operation.Request)
	require.Len(t, operation.Request.Contents, 1)
	for _, pe := range operation.Request.Contents[0].Encoding {
		require.Len(t, pe.Headers, 1)
		return pe.Headers[0]
	}
	t.Fatal("no header at either position")
	return ir.Header{}
}

// assertHeaderSerialization requires both keywords to have lowered onto the
// header declared at `at`, neither kept verbatim nor announced.
func assertHeaderSerialization(t *testing.T, header ir.Header, diags []ir.Diagnostic, at string) {
	t.Helper()
	assert.Equal(t, jsontext.Pointer(at), header.Provenance.Pointer)
	assert.Equal(t, "simple", header.Style)
	require.NotNil(t, header.Explode)
	assert.True(t, *header.Explode)
	assert.NotContains(t, header.Unmodeled, "openapi:style")
	assert.NotContains(t, header.Unmodeled, "openapi:explode")
	assert.False(t, openapitest.HasDiag(diags, diag.DegradedConstruct), "nothing is degraded: %+v", diags)
}

// TestHeaders_NotesAreTheHeadersWhateverItsSchemaHoists pins a header as the
// home of what its schema notes where that schema hoists a node: in a response,
// in a component and in a multipart part's encoding, each schema a list, an
// enum or a model, the header holds the deprecation, and the documentation and
// examples unless it writes its own, the node keeping the schema's then, so
// none is lost (docs/ir/annotations.md#one-structural-home-per-declaration),
// and a plain use of a component one use describes reads them on the node.
// Both orders of the components compile alike.
func TestHeaders_NotesAreTheHeadersWhateverItsSchemaHoists(t *testing.T) {
	t.Parallel()
	const notes = "description: DOC, deprecated: true, examples: [[a]], xml: {name: X}"
	const schema = "{type: array, items: {type: string}, " + notes + "}"
	components := []string{
		"Shared: {schema: " + schema + "}",
		"Described: {description: OWN, schema: {type: object, properties: {a: {type: string}}, " + notes + "}}",
		"Used: {schema: " + schema + "}",
		"Summed: {schema: " + schema + "}",
	}
	compile := func(components []string) (*ir.Document, ir.Service) {
		doc, svc, diags := compileService(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"+
			"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-Inline: {schema: "+schema+"}\n"+
			"            X-Own: {description: OWN, schema: "+schema+"}\n"+
			"            X-En: {description: OWN, schema: {type: string, enum: [a, b], "+notes+"}}\n"+
			"            X-Example: {example: [b], schema: "+schema+"}\n"+
			"            X-Entry: {content: {text/plain: {example: [b], schema: "+schema+"}}}\n"+
			"            X-Shared: {$ref: '#/components/headers/Shared'}\n"+
			"            X-Described: {$ref: '#/components/headers/Described'}\n"+
			"            X-Used: {$ref: '#/components/headers/Used', description: USE}\n"+
			"            X-Plain: {$ref: '#/components/headers/Used'}\n"+
			"            X-Summed: {$ref: '#/components/headers/Summed', summary: SUM}\n"+
			"    post:\n      operationId: p\n      requestBody:\n        content:\n          multipart/form-data:\n"+
			"            schema: {type: object, properties: {file: {type: string}}}\n"+
			"            encoding:\n              file:\n                headers:\n"+
			"                  X-Part: {schema: "+schema+"}\n"+
			"      responses: {\"204\": {description: none}}\n"+
			"components:\n  headers:\n    "+strings.Join(components, "\n    ")+"\n")
		openapitest.RequireNoErrorDiags(t, diags)
		return doc, svc
	}
	doc, svc := compile(components)
	headers := headersByWire(svc)
	for _, tc := range []struct {
		wire, docs, nodeDocs   string
		examples, nodeExamples []string
	}{
		{"X-Inline", "DOC", "", []string{"a"}, nil},
		{"X-Own", "OWN", "DOC", []string{"a"}, nil},
		{"X-En", "OWN", "DOC", []string{"a"}, nil},
		{"X-Example", "DOC", "", []string{"b"}, []string{"a"}},
		{"X-Entry", "DOC", "", []string{"b"}, []string{"a"}},
		{"X-Shared", "DOC", "", []string{"a"}, nil},
		{"X-Described", "OWN", "DOC", []string{"a"}, nil},
		{"X-Used", "USE", "DOC", []string{"a"}, nil},
		{"X-Plain", "", "DOC", []string{"a"}, nil},
		{"X-Summed", "", "DOC", []string{"a"}, nil},
		{"X-Part", "DOC", "", []string{"a"}, nil},
	} {
		header, ok := headers[tc.wire]
		require.True(t, ok, tc.wire)
		assert.Equal(t, tc.docs, header.Docs.Description, tc.wire)
		assert.Equal(t, tc.examples, listedExamples(header.Examples), tc.wire)
		assert.NotNil(t, header.Deprecation, tc.wire)
		assert.Nil(t, header.XML, "%s: the node keeps its XML hints", tc.wire)

		node, ok := doc.Types[header.Type.Target]
		require.True(t, ok, "%s: the schema hoists a node", tc.wire)
		require.True(t, node.Common().Anonymous, tc.wire)
		assert.Equal(t, tc.nodeDocs, node.Common().Docs.Description, tc.wire)
		assert.Equal(t, tc.nodeExamples, listedExamples(node.Common().Examples), tc.wire)
		assert.Nil(t, node.Common().Deprecation, tc.wire)
		assert.NotNil(t, node.Common().XML, tc.wire)
	}

	reversed := slices.Clone(components)
	slices.Reverse(reversed)
	reversedDoc, reversedSvc := compile(reversed)
	assert.Empty(t, cmp.Diff(doc.Types, reversedDoc.Types), "the types do not depend on component order")
	assert.Empty(t, cmp.Diff(svc, reversedSvc), "nor do the headers")
}

// headersByWire returns every header svc's operations declare, in a response or a
// request part's encoding, by wire name.
func headersByWire(svc ir.Service) map[string]ir.Header {
	headers := map[string]ir.Header{}
	for _, operation := range svc.Groups[0].Operations {
		for _, response := range operation.Responses {
			for _, header := range response.Headers {
				headers[header.WireName] = header
			}
		}
		if operation.Request == nil {
			continue
		}
		for _, content := range operation.Request.Contents {
			for _, encoding := range content.Encoding {
				for _, header := range encoding.Headers {
					headers[header.WireName] = header
				}
			}
		}
	}
	return headers
}
