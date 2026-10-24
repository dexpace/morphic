package operation_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestSingleContentEntry_ReportsExtraMediaTypes covers the invalid document
// OpenAPI forbids: a content-style header or parameter declaring more than one
// media type. Only the first can lower — ir.Property and ir.Parameter each hold
// one type — so the rest were dropped without a word until the position that
// takes the first started naming what it left (GitHub #139).
func TestSingleContentEntry_ReportsExtraMediaTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		at   string
	}{
		{
			name: "response header",
			spec: openapitest.PathsSpec("  /x:\n    get:\n      responses:\n" +
				"        \"200\":\n          description: ok\n          headers:\n" +
				"            H:\n              content:\n" +
				"                application/xml: {schema: {type: string}}\n" +
				"                application/json: {schema: {type: integer}}\n"),
			at: "/paths/~1x/get/responses/200/headers/H/content",
		},
		{
			name: "operation parameter",
			spec: openapitest.PathsSpec("  /x:\n    get:\n      parameters:\n" +
				"        - name: p\n          in: query\n          content:\n" +
				"              application/xml: {schema: {type: string}}\n" +
				"              application/json: {schema: {type: integer}}\n" +
				"      responses: {\"200\": {description: ok}}\n"),
			at: "/paths/~1x/get/parameters/0/content",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, tc.spec)
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, tc.at),
				"the ignored media types are named at the content map: %+v", diags)
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, tc.at),
				"application/json", "the message names what was ignored, not only that something was")
		})
	}
}

// TestSingleContentEntry_OneEntryIsSilent is the control: the legal one-entry
// spelling must not warn, or every content-style header and parameter would.
func TestSingleContentEntry_OneEntryIsSilent(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.PathsSpec("  /x:\n    get:\n      responses:\n"+
		"        \"200\":\n          description: ok\n          headers:\n"+
		"            H:\n              content:\n"+
		"                application/xml: {schema: {type: string}}\n"))
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"one media type is the legal spelling: %+v", diags)
}

// TestElectTypeSpelling_NeitherSpelling covers a header that declares no type at
// all. It is legal — a header may carry only a description — and must lower to
// the top type without reporting a loss, since nothing was written to lose.
func TestElectTypeSpelling_NeitherSpelling(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec("  /x:\n    get:\n      operationId: untypedHeader\n      responses:\n"+
		"        \"200\":\n          description: ok\n          headers:\n"+
		"            H: {description: untyped}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	headers := openapitest.FindOp(t, doc, "untypedHeader").Responses[0].Headers
	require.Len(t, headers, 1)
	assert.Equal(t, ir.TypeID("t/prim/any"), headers[0].Type.Target,
		"a header declaring no type lowers to the top type")
	assert.Nil(t, headers[0].Encoding, "no content map means no media type")
	assert.Equal(t, "untyped", headers[0].Docs.Description)
}

// TestElectTypeSpelling_CoDeclaredSpellingsElectContent covers the invalid
// document OpenAPI forbids at the two positions one rule governs: a parameter,
// and a header which follows the parameter rules, declaring `schema` and
// `content` together. The table reports the rule broken, and each position
// holds one type, so one spelling is elected and the other kept verbatim beside
// it — where both positions used to take one in silence, and in opposite
// directions (GitHub #320).
func TestElectTypeSpelling_CoDeclaredSpellingsElectContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    string
		at      string
		elected func(*testing.T, *ir.Document) (ir.TypeID, string, ir.Unmodeled)
	}{
		{
			name: "operation parameter",
			spec: openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      parameters:
        - name: p
          in: query
          schema: {type: integer}
          content:
            application/json: {schema: {type: string}}
      responses: {"200": {description: ok}}
`),
			at:      "/paths/~1x/get/parameters/0",
			elected: electedParam,
		},
		{
			name: "response header",
			spec: openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      responses:
        "200":
          description: ok
          headers:
            X-H:
              schema: {type: integer}
              content:
                application/json: {schema: {type: string}}
`),
			at:      "/paths/~1x/get/responses/200/headers/X-H",
			elected: electedHeader,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)
			assert.Equal(t, []string{"openapi/validation/validation-mutually-exclusive-fields at " + tc.at},
				errorFindings(diags))
			target, mediaType, unmodeled := tc.elected(t, doc)

			assert.Equal(t, ir.TypeID("t/prim/string"), target,
				"the content entry's schema is the type, not the schema written beside it")
			assert.Equal(t, "application/json", mediaType,
				"and the media type it names reaches the field that models it")

			entry, ok := unmodeled["openapi:schema"]
			require.True(t, ok, "the passed-over schema is kept verbatim; got %v", unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.JSONEq(t, `{"type":"integer"}`, string(entry.Value))
			assert.Equal(t, jsontext.Pointer(tc.at+"/schema"), entry.Provenance.Pointer,
				"located at the keyword itself, not at the object that carried it")

			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, tc.at+"/schema")
			assert.Contains(t, msg, "lowered as its content", "the message names the elected spelling")
			assert.Contains(t, msg, "with schema kept verbatim", "and the one it passed over")
		})
	}
}

// TestElectTypeSpelling_SoleSpellingReportsNothing is the control. A position
// that writes one spelling has no election to make, so it must keep nothing and
// say nothing — otherwise every well-formed parameter and header in every
// document would carry a residue entry and a warning.
func TestElectTypeSpelling_SoleSpellingReportsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      parameters:
        - name: bySchema
          in: query
          schema: {type: string}
        - name: byContent
          in: query
          content:
            application/json: {schema: {type: string}}
      responses:
        "200":
          description: ok
          headers:
            X-Schema: {schema: {type: string}}
            X-Content:
              content:
                application/json: {schema: {type: string}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"one spelling is the legal form, so there is no election to announce: %+v", diags)

	op := openapitest.FindOp(t, doc, "getX")
	require.Len(t, op.Params, 2)
	for _, p := range op.Params {
		assertNoPassedOverSpelling(t, p.Unmodeled, p.Name.Source)
	}
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 2)
	for _, h := range op.Responses[0].Headers {
		assertNoPassedOverSpelling(t, h.Unmodeled, h.WireName)
	}
}

// TestElectTypeSpelling_ElectedContentWithoutASchemaStillWins pins the edge the
// election's rationale is stated against: `content` is elected because it is the
// more expressive spelling, and the rule holds even at the one shape where it is
// not — an entry naming a media type but no schema.
//
// The type is `any` and the declared integer sits beside it, rather than the
// other way round. Electing per entry would recover this case and lose what the
// election is for: two documents alike but for whether an entry names a schema
// would then elect different spellings, which is the disagreement being fixed.
func TestElectTypeSpelling_ElectedContentWithoutASchemaStillWins(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      parameters:
        - name: p
          in: query
          schema: {type: integer}
          content:
            application/json: {}
      responses: {"200": {description: ok}}
`))
	op := openapitest.FindOp(t, doc, "getX")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.TypeID("t/prim/any"), op.Params[0].Type.Target,
		"the elected content states no schema, so the position states no type")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)
	assert.Equal(t, "application/json", op.Bindings.HTTP[0].ParamBindings[0].ContentType,
		"the media type it does state is still modelled")

	at := "/paths/~1x/get/parameters/0/schema"
	entry, ok := op.Params[0].Unmodeled["openapi:schema"]
	require.True(t, ok, "and the passed-over schema is kept; got %v", op.Params[0].Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"type":"integer"}`, string(entry.Value))
	assert.Equal(t, jsontext.Pointer(at), entry.Provenance.Pointer)

	msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, at)
	assert.Contains(t, msg, "lowered as its content", "the message names the elected spelling")
}

// TestElectTypeSpelling_UnusableContentElectsSchemaAndKeepsIt covers the other
// direction. A `content` map yielding no entry states no type, so the schema is
// elected instead — but the document declared both spellings either way, and the
// one passed over is kept and named exactly as it is when content wins.
//
// The library's validator reports the empty map beside this, which is the same
// invalidity seen from its angle rather than a second finding: Compile lowers the
// whole document rather than stopping at it, so the assertions below are reached.
func TestElectTypeSpelling_UnusableContentElectsSchemaAndKeepsIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      parameters:
        - name: p
          in: query
          schema: {type: integer}
          content: {}
      responses: {"200": {description: ok}}
`))
	op := openapitest.FindOp(t, doc, "getX")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.TypeID("t/prim/integer"), op.Params[0].Type.Target,
		"the schema is the type, since the content map names none")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)
	assert.Empty(t, op.Bindings.HTTP[0].ParamBindings[0].ContentType,
		"and there is no media type to record")

	at := "/paths/~1x/get/parameters/0/content"
	entry, ok := op.Params[0].Unmodeled["openapi:content"]
	require.True(t, ok, "the passed-over content map is kept verbatim; got %v", op.Params[0].Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{}`, string(entry.Value))
	assert.Equal(t, jsontext.Pointer(at), entry.Provenance.Pointer)

	msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, at)
	assert.Contains(t, msg, "lowered as its schema", "the message names the elected spelling")
	assert.Contains(t, msg, "with content kept verbatim", "and the one it passed over")
}

// TestParamContent_MergedFieldIsKept pins that a content-only field merged into
// the media type through `<<` is kept like one written directly: the parser
// models the field, so nothing else would report it.
func TestParamContent_MergedFieldIsKept(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json:
              <<: {itemSchema: {type: string}}
              schema: {type: integer}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	kept, ok := op.Params[0].Unmodeled["openapi:content/application~1json/itemSchema"]
	require.True(t, ok, "got %v", slices.Sorted(maps.Keys(op.Params[0].Unmodeled)))
	assert.JSONEq(t, `{"type":"string"}`, string(kept.Value))
}

// TestParamContent_MediaTypeFieldsReachTheParameter pins GitHub #611 at the
// parameter position: electing a `content` entry used to read only the media
// type's schema, so the object's example/examples, its x-*, its undeclared keys
// and the parser-modelled fields no ir.Parameter holds vanished with no field,
// no Unmodeled entry and no diagnostic.
func TestParamContent_MediaTypeFieldsReachTheParameter(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /search:
    get:
      operationId: search
      parameters:
        - name: filter
          in: query
          content:
            application/json:
              schema: {type: object, properties: {kind: {type: string}}}
              examples:
                one: {summary: One, value: {kind: a}}
              itemSchema: {type: string}
              x-note: note
              bogus: B
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	param := op.Params[0]

	require.Len(t, param.Examples, 1, "the media type's own examples reach the parameter")
	assert.Equal(t, "one", param.Examples[0].Name)
	assert.Equal(t, "One", param.Examples[0].Summary)
	require.NotNil(t, param.Examples[0].Value)

	key := func(name string) string { return "openapi:content/application~1json/" + name }
	assert.Equal(t, ir.ReasonVendorExtension, param.Unmodeled[key("x-note")].Reason,
		"the media type's x-* survives under the content entry's own scope; got %v",
		slices.Sorted(maps.Keys(param.Unmodeled)))
	assert.Equal(t, ir.ReasonNoIRHome, param.Unmodeled[key("itemSchema")].Reason,
		"a parser-modelled field with no ir.Parameter home is kept verbatim rather than dropped")
	assert.Equal(t, ir.ReasonOutOfScope, param.Unmodeled[key("bogus")].Reason,
		"an undefined key keeps today's grading")
	openapitest.AssertInfoDiagAt(t, diags, "/paths/~1search/get/parameters/0/content/application~1json/itemSchema")
}

// TestHeaderContent_MediaTypeFieldsReachTheProperty is the header half of GitHub
// #611: the same one-field read is what dropped a content-style header's media
// type fields, so the same fix reaches them on the Property.
func TestHeaderContent_MediaTypeFieldsReachTheProperty(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /reports:
    get:
      operationId: getReport
      responses:
        "200":
          description: ok
          headers:
            X-Report:
              content:
                application/json:
                  schema: {type: object, properties: {hits: {type: integer}}}
                  examples:
                    hit: {summary: One hit, value: {hits: 1}}
                  itemSchema: {type: string}
                  x-hdr: hdr
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	header := openapitest.FirstOp(t, svc).Responses[0].Headers[0]

	require.Len(t, header.Examples, 1, "the media type's own examples reach the header")
	assert.Equal(t, "hit", header.Examples[0].Name)
	assert.Equal(t, "One hit", header.Examples[0].Summary)
	key := func(name string) string { return "openapi:content/application~1json/" + name }
	assert.Equal(t, ir.ReasonVendorExtension, header.Unmodeled[key("x-hdr")].Reason)
	assert.Equal(t, ir.ReasonNoIRHome, header.Unmodeled[key("itemSchema")].Reason)
	openapitest.AssertInfoDiagAt(t, diags, "/paths/~1reports/get/responses/200/headers/X-Report/content/application~1json/itemSchema")
}

// TestParamAndHeaderSchema_NeverRecordContentFields is the passed-over case: the
// schema spelling elects no media type, so nothing content-scoped can appear on
// the carrier and no info is reported.
func TestParamAndHeaderSchema_NeverRecordContentFields(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /plain:
    get:
      operationId: plain
      parameters:
        - {name: filter, in: query, schema: {type: string}}
      responses:
        "200":
          description: ok
          headers:
            X-Plain: {schema: {type: string}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1)
	for key := range op.Params[0].Unmodeled {
		assert.NotContains(t, key, "openapi:content/", "the schema spelling elects no media type")
	}
	assert.Empty(t, op.Params[0].Examples)
	require.Len(t, op.Responses[0].Headers, 1)
	for key := range op.Responses[0].Headers[0].Unmodeled {
		assert.NotContains(t, key, "openapi:content/")
	}
	assert.Empty(t, op.Responses[0].Headers[0].Examples)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"nothing was passed over, so nothing is announced")
}

// electedParam reads what the election left on the operation's single
// parameter: its type, the media type on its binding, and its Unmodeled.
func electedParam(t *testing.T, doc *ir.Document) (ir.TypeID, string, ir.Unmodeled) {
	t.Helper()
	op := openapitest.FindOp(t, doc, "getX")
	require.Len(t, op.Params, 1)
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1)
	return op.Params[0].Type.Target, op.Bindings.HTTP[0].ParamBindings[0].ContentType, op.Params[0].Unmodeled
}

// electedHeader reads the same three things off the operation's single response
// header, where the media type has a different home.
func electedHeader(t *testing.T, doc *ir.Document) (ir.TypeID, string, ir.Unmodeled) {
	t.Helper()
	responses := openapitest.FindOp(t, doc, "getX").Responses
	require.Len(t, responses, 1)
	headers := responses[0].Headers
	require.Len(t, headers, 1)
	require.NotNil(t, headers[0].Encoding, "a content-style header records its media type")
	return headers[0].Type.Target, headers[0].Encoding.MediaType, headers[0].Unmodeled
}

// assertNoPassedOverSpelling requires that neither spelling was kept verbatim at
// a position that declared only one of them.
func assertNoPassedOverSpelling(t *testing.T, u ir.Unmodeled, at string) {
	t.Helper()
	assert.NotContains(t, u, "openapi:schema", "%s kept a schema it never passed over", at)
	assert.NotContains(t, u, "openapi:content", "%s kept a content map it never passed over", at)
}
