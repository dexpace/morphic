// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// linkSpec is a document whose operation src carries link on its 200 response,
// and bare, which declares no body, on its own. target declares id in its path
// and its query, so an unqualified id names it ambiguously.
func linkSpec(srcLink, bareLink string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /src/{sid}:
    post:
      operationId: src
      parameters:
        - {name: sid, in: path, required: true, schema: {type: string}}
      requestBody:
        content:
          application/json: {schema: {$ref: '#/components/schemas/M'}}
      responses:
        "200":
          description: ok
          headers: {X-H: {schema: {type: string}}}
          content:
            application/json: {schema: {$ref: '#/components/schemas/M'}}
          links: {L: ` + srcLink + `}
  /bare:
    get:
      operationId: bare
      responses:
        "200":
          description: ok
          links: {L: ` + bareLink + `}
  /t/{id}:
    get:
      operationId: target
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
        - {name: id, in: query, schema: {type: string}}
        - {name: X-Up, in: header, schema: {type: string}}
      responses: {"200": {description: ok}}
  /dup1: {get: {operationId: dup, responses: {"200": {description: ok}}}}
  /dup2: {get: {operationId: dup, responses: {"200": {description: ok}}}}
  /mount1: {$ref: '#/components/pathItems/Twice'}
  /mount2: {$ref: '#/components/pathItems/Twice'}
  /mount3: {$ref: '#/components/pathItems/Once'}
components:
  pathItems:
    Twice: {get: {responses: {"200": {description: ok}}}}
    Once: {get: {operationId: once, responses: {"200": {description: ok}}}}
  schemas:
    M:
      type: object
      properties:
        id: {type: string}
        list: {type: array, items: {$ref: '#/components/schemas/M'}}
        map: {type: object, additionalProperties: {type: string}}
        u: {oneOf: [{$ref: '#/components/schemas/M'}, {type: string}]}
`
}

// srcLinkPointer is where linkSpec's link on src is reported.
const srcLinkPointer = "/paths/~1src~1{sid}/post/responses/200/links/L"

// keptLink compiles a linkSpec carrying link on the operation on, and returns
// that response and the findings at its link.
func keptLink(t *testing.T, on, link string) (ir.Response, []ir.Diagnostic) {
	t.Helper()
	srcLink, bareLink := link, "{operationId: target, parameters: {path.id: a}}"
	if on == "bare" {
		srcLink, bareLink = bareLink, link
	}
	doc, diags := parseFull(t, linkSpec(srcLink, bareLink))
	resp := openapitest.FindOp(t, doc, on).Responses[0]
	pointer := srcLinkPointer
	if on == "bare" {
		pointer = "/paths/~1bare/get/responses/200/links/L"
	}
	var at []ir.Diagnostic
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) {
			at = append(at, d)
		}
	}
	return resp, at
}

// TestLinks_AFindingKeepsTheLinkWhole holds every link that does not lower to
// the same outcome: no link on the response, the link verbatim under its own
// key, and one finding at its entry in the links map, a warning for a link that
// names nothing or breaks a grammar and an info for a sound one no
// ir.LinkValue spells.
func TestLinks_AFindingKeepsTheLinkWhole(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, on, link, code, says string
	}{
		{"named both ways", "src", "{operationId: target, operationRef: '#/paths/~1t~1{id}/get'}", diag.InvalidLink, "by both operationId and operationRef"},
		{"named neither way", "src", "{description: d}", diag.InvalidLink, "names no operation"},
		{"an operationId nothing declares", "src", "{operationId: nope}", diag.InvalidLink, `operationId "nope", which no operation`},
		{"an operationId two declare", "src", "{operationId: dup}", diag.InvalidLink, `operationId "dup", which 2 operations declare`},
		{"an operationRef to no operation", "src", "{operationRef: '#/components/schemas/M'}", diag.InvalidLink, "which no operation"},
		{"a parameter the target lacks", "src", "{operationId: target, parameters: {nope: a}}", diag.InvalidLink, `parameter "nope"`},
		{"a name two locations share", "src", "{operationId: target, parameters: {id: a}}", diag.InvalidLink, "declares in 2 locations"},
		{"a dotted name no location starts", "src", "{operationId: target, parameters: {x.id: a}}", diag.InvalidLink, `parameter "x.id"`},
		{"a dotted name a non-parameter location starts", "src", "{operationId: target, parameters: {body.id: a}}", diag.InvalidLink, `parameter "body.id"`},
		{"one parameter twice", "src", "{operationId: target, parameters: {X-Up: a, x-up: b}}", diag.InvalidLink, `as "X-Up" and again as "x-up"`},
		{"a malformed expression", "src", "{operationId: target, parameters: {path.id: '$request.cookie.c'}}", diag.InvalidLink, "breaks the runtime expression grammar"},
		{"a response's query", "src", "{operationId: target, parameters: {path.id: '$response.query.q'}}", diag.InvalidLink, "which no response carries"},
		{"a malformed body", "src", "{operationId: target, requestBody: '$response.body#id'}", diag.InvalidLink, "breaks the runtime expression grammar"},
		{"another document's operation", "src", "{operationRef: 'other.yaml#/paths/~1x/get'}", diag.UntypedLink, "not one this document declares"},
		{"an operation mounted twice", "src", "{operationRef: '#/components/pathItems/Twice/get'}", diag.UntypedLink, "mounted 2 times"},
		{"a request parameter not bound", "src", "{operationId: target, parameters: {path.id: '$request.query.q'}}", diag.UntypedLink, `query parameter "q"`},
		{"a header the response lacks", "src", "{operationId: target, parameters: {path.id: '$response.header.X-Gone'}}", diag.UntypedLink, `header "X-Gone"`},
		{"a request body never described", "bare", "{operationId: target, parameters: {path.id: '$request.body'}}", diag.UntypedLink, "request body the document does not describe"},
		{"a response body never described", "bare", "{operationId: target, parameters: {path.id: '$response.body#/id'}}", diag.UntypedLink, "response body the document does not describe"},
		{"an array index", "src", "{operationId: target, parameters: {path.id: '$response.body#/list/0/id'}}", diag.UntypedLink, "no chain of members"},
		{"a map key", "src", "{operationId: target, parameters: {path.id: '$response.body#/map/k'}}", diag.UntypedLink, "no chain of members"},
		{"a union's member", "src", "{operationId: target, parameters: {path.id: '$response.body#/u/id'}}", diag.UntypedLink, "no chain of members"},
		{"a member no model declares", "src", "{operationId: target, parameters: {path.id: '$response.body#/gone'}}", diag.UntypedLink, "no chain of members"},
		{"a template", "src", "{operationId: target, parameters: {path.id: 'item-{$response.body#/id}'}}", diag.UntypedLink, "an expression is substituted into"},
		{"a value under a custom tag", "src", "{operationId: target, parameters: {path.id: !acme thing}}", diag.UntypedLink, "no constant for"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, at := keptLink(t, tc.on, tc.link)
			assert.Empty(t, resp.Links, "a link that does not lower whole is not lowered in part")
			assert.Contains(t, resp.Unmodeled, "openapi:links/L", "it is kept verbatim under its own key")
			var found []string
			for _, d := range at {
				if d.Code == tc.code {
					found = append(found, d.Message)
					assert.Equal(t, map[string]ir.Severity{diag.InvalidLink: ir.SeverityWarning, diag.UntypedLink: ir.SeverityInfo}[tc.code],
						d.Severity, "a link kept verbatim fails no compile")
				}
			}
			require.Len(t, found, 1, "one finding at the link's entry; got %+v", at)
			assert.Contains(t, found[0], tc.says)
			assert.Contains(t, found[0], "kept verbatim under Unmodeled")
		})
	}
}

// TestLinks_ResolvedFormsLower pins the forms no conformance case writes: an
// operationRef to the one mount of a declaration, a key naming its location
// that falls back to a parameter named with the dot, and a constant beside an
// expression in a link the response's default carries.
func TestLinks_ResolvedFormsLower(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, linkSpec("{operationRef: '#/components/pathItems/Once/get'}",
		"{operationId: target, parameters: {header.X-Up: 'x', query.id: 2}}"))
	assert.Empty(t, severitiesOf(diags, diag.InvalidLink), "the document's duplicate operationId is its own finding")
	assert.Empty(t, severitiesOf(diags, diag.UntypedLink))
	src := openapitest.FindOp(t, doc, "src").Responses[0]
	require.Len(t, src.Links, 1)
	assert.Equal(t, openapitest.FindOp(t, doc, "once").ID, src.Links[0].Operation,
		"a declaration mounted once is the operation an operationRef to it names")

	bare := openapitest.FindOp(t, doc, "bare").Responses[0]
	require.Len(t, bare.Links, 1)
	target := openapitest.FindOp(t, doc, "target")
	require.Len(t, bare.Links[0].Params, 2)
	assert.Equal(t, target.Params[2].ID, bare.Links[0].Params[0].Param, "header.X-Up names the header")
	assert.Equal(t, target.Params[1].ID, bare.Links[0].Params[1].Param, "query.id names the query parameter alone")
	assert.Equal(t, &ir.Value{Kind: ir.ValueNumber, Num: "2"}, bare.Links[0].Params[1].Value.Constant)
}

// TestLinks_EveryValueKindLowers pins each kind of value a link passes at what
// it reads, on links no conformance case's operation package test reaches: an
// operationRef to a mount, the server a link names, and the request body whole.
func TestLinks_EveryValueKindLowers(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, linkSpec(
		"{operationRef: '#/paths/~1t~1{id}/get', requestBody: '$request.body', server: {url: 'https://s.example'},"+
			" parameters: {path.id: '$response.body#/id', X-Up: '$response.header.x-h', query.id: '$url'}}",
		"{operationId: target, parameters: {path.id: '$method', query.id: '$statusCode'}}"))
	assert.Empty(t, severitiesOf(diags, diag.InvalidLink))
	assert.Empty(t, severitiesOf(diags, diag.UntypedLink))
	target := openapitest.FindOp(t, doc, "target")
	src := openapitest.FindOp(t, doc, "src")

	link := src.Responses[0].Links
	require.Len(t, link, 1)
	assert.Equal(t, target.ID, link[0].Operation, "an operationRef to a mount names the operation mounted there")
	require.NotNil(t, link[0].Server)
	assert.Equal(t, "https://s.example", doc.Servers[*link[0].Server].URLTemplate)
	assert.Equal(t, &ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{}}, link[0].Body, "the request body whole")
	assert.Equal(t, []ir.LinkParam{
		{Param: target.Params[0].ID, Value: ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{
			Segments: []ir.PropID{"p/openapi/components/schemas/M/properties/id"},
		}}},
		{Param: target.Params[2].ID, Value: ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{
			In: "header", Segments: []ir.PropID{src.Responses[0].Headers[0].ID},
		}}},
		{Param: target.Params[1].ID, Value: ir.LinkValue{Kind: ir.LinkURL}},
	}, link[0].Params)

	bare := openapitest.FindOp(t, doc, "bare").Responses[0].Links
	require.Len(t, bare, 1)
	assert.Equal(t, []ir.LinkParam{
		{Param: target.Params[0].ID, Value: ir.LinkValue{Kind: ir.LinkMethod}},
		{Param: target.Params[1].ID, Value: ir.LinkValue{Kind: ir.LinkStatus}},
	}, bare[0].Params)
}

// TestLinks_ErrorsAndDefaultsCarryLinks pins that an error response and the
// default lower their links as a success response does.
func TestLinks_ErrorsAndDefaultsCarryLinks(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get:
      operationId: a
      responses:
        "404": {description: gone, links: {Again: {operationId: a}}}
        default: {description: any, links: {Again: {operationId: a, x-k: v}}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "a")
	require.Len(t, op.Errors, 1)
	require.Len(t, op.Errors[0].Links, 1)
	assert.Equal(t, op.ID, op.Errors[0].Links[0].Operation)
	require.NotNil(t, op.Default)
	require.Len(t, op.Default.Links, 1)
	assert.Contains(t, op.Default.Links[0].Unmodeled, "openapi:x-k")
}

// TestLinks_ASharedResponseResolvesPerOperation pins that one response two
// operations $ref resolves its link against each: what the link reads of the
// request is the parameter of the operation it answers.
func TestLinks_ASharedResponseResolvesPerOperation(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a/{id}:
    get:
      operationId: a
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      responses: {"200": {$ref: '#/components/responses/R'}}
  /b/{id}:
    get:
      operationId: b
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      responses: {"200": {$ref: '#/components/responses/R'}}
components:
  responses:
    R:
      description: ok
      links: {Self: {operationId: a, parameters: {id: '$request.path.id'}}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"a", "b"} {
		op := openapitest.FindOp(t, doc, name)
		require.Len(t, op.Responses[0].Links, 1, name)
		value := op.Responses[0].Links[0].Params[0].Value
		require.NotNil(t, value.Request, name)
		assert.Equal(t, &op.Params[0].ID, value.Request.Param, "%s's link reads %s's own parameter", name, name)
	}
}

// TestLinks_MediaTypesDisagreeingOnAMember pins that a body read is typed only
// when every content walks it to the same members: the value is one whichever
// media type arrives.
func TestLinks_MediaTypesDisagreeingOnAMember(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {type: object, properties: {id: {type: string}}}}
            application/xml: {schema: {type: object, properties: {id: {type: string}}}}
          links: {Again: {operationId: a, requestBody: '$response.body#/id'}}
`))
	resp := openapitest.FindOp(t, doc, "a").Responses[0]
	assert.Empty(t, resp.Links)
	assert.Contains(t, resp.Unmodeled, "openapi:links/Again")
	assert.Equal(t, []ir.Severity{ir.SeverityInfo}, severitiesOf(diags, diag.UntypedLink))
}

// TestLinks_LowerAlikeInEitherDeclarationOrder compiles one document with its
// paths declared in both orders and diffs every operation's links and the
// findings about them. The harness's order oracle reads no operation, so this
// is the two-order diff for links: a link naming an operation declared after
// it, one naming an operationId two operations declare, and one reading the
// request.
func TestLinks_LowerAlikeInEitherDeclarationOrder(t *testing.T) {
	t.Parallel()
	const src = `  /src/{id}:
    get:
      operationId: src
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      responses:
        "200":
          description: ok
          links:
            Next: {operationId: target, parameters: {id: '$request.path.id'}}
            Either: {operationId: twin}
`
	const rest = `  /t/{id}:
    get:
      operationId: target
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      responses: {"200": {description: ok, links: {Back: {operationId: src, parameters: {id: '$request.path.id'}}}}}
  /twin1: {get: {operationId: twin, responses: {"200": {description: ok}}}}
  /twin2: {get: {operationId: twin, responses: {"200": {description: ok}}}}
`
	links := func(spec string) (map[ir.OpID][]ir.OperationLink, []string) {
		doc, diags := parseFull(t, openapitest.PathsSpec(spec))
		got := map[ir.OpID][]ir.OperationLink{}
		ir.ForEachOperation(doc, func(op ir.Operation) {
			for _, r := range op.Responses {
				got[op.ID] = append(got[op.ID], r.Links...)
			}
		})
		var findings []string
		for _, d := range diags {
			if d.Code == diag.InvalidLink || d.Code == diag.UntypedLink {
				findings = append(findings, d.Code+" "+string(d.Provenance.Pointer))
			}
		}
		return got, findings
	}
	firstLinks, firstFindings := links(src + rest)
	secondLinks, secondFindings := links(rest + src)
	require.Len(t, firstLinks["op/openapi/paths/~1src~1{id}/get"], 1, "the link to target lowers")
	assert.Empty(t, cmp.Diff(firstLinks, secondLinks), "links depend on declaration order")
	assert.ElementsMatch(t, firstFindings, secondFindings, "findings depend on declaration order")
	assert.Equal(t, []string{diag.InvalidLink + " /paths/~1src~1{id}/get/responses/200/links/Either"}, firstFindings)
}

// severitiesOf returns the severity of each finding of diags under code.
func severitiesOf(diags []ir.Diagnostic, code string) []ir.Severity {
	var out []ir.Severity
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Severity)
		}
	}
	return out
}
