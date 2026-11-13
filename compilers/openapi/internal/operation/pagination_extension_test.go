// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// pagePolicy maps every pagination target to a member of x-page, holding an
// expression, and the values-reading ones also to the word x-role's purpose
// writes, marking the node it sits on.
func pagePolicy() openapi.ExtensionPromotions {
	expression := func(member string) openapi.ExtensionSource {
		return openapi.ExtensionSource{Key: "x-page", Member: member}
	}
	mark := func(word string) openapi.ExtensionSource {
		return openapi.ExtensionSource{Key: "x-role", Member: "purpose", Values: []string{word}}
	}
	return openapi.ExtensionPromotions{Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
		openapi.TargetPaginationCursor:     {expression("cursor"), mark("cursor")},
		openapi.TargetPaginationOffset:     {expression("offset")},
		openapi.TargetPaginationPage:       {expression("page")},
		openapi.TargetPaginationLimit:      {expression("limit"), mark("limit")},
		openapi.TargetPaginationItems:      {expression("items"), mark("items")},
		openapi.TargetPaginationNextCursor: {expression("next"), mark("next")},
		openapi.TargetPaginationNextLink:   {expression("link")},
		openapi.TargetPaginationTotalCount: {expression("total")},
		openapi.TargetPaginationTotalPages: {expression("pages")},
	}}
}

// pagedSpec is a document whose one operation, x, writes op beneath its
// operationId: the extensions, parameters, body and responses a case reads.
func pagedSpec(op string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    post:
      operationId: x
` + op + `
components:
  schemas:
    Page:
      type: object
      properties:
        items: {type: array, items: {type: string}}
        info: {$ref: '#/components/schemas/Info'}
        next: {type: string}
    Info: {type: object, properties: {next: {type: string}, total: {type: integer}}}
    Query: {type: object, properties: {cursor: {type: string}, size: {type: integer}}}
    Token: {type: string, x-role: {purpose: next}}
    Cursor: {type: string, x-role: {purpose: cursor}}
    Marked: {type: object, x-role: {purpose: items}, properties: {next: {type: string}}}
    Tokens: {type: object, properties: {token: {$ref: '#/components/schemas/Token'}}}
`
}

// pageResponse is a success response whose body is a Page.
const pageResponse = `      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Page'}}}`

// compilePaged compiles a pagedSpec under policy and returns operation x.
func compilePaged(t *testing.T, op string, policy openapi.ExtensionPromotions) (*ir.Document, ir.Operation, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(pagedSpec(op))},
		compilers.Options{FormatOptions: openapi.Options{Promotions: policy}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, openapitest.FirstOp(t, doc.Services[0]), diags
}

// renderPage spells p by the names its fields reach, so a case reads as the
// document it compiles: a parameter as param:<name>, the request body as
// body:<members>, and a page value as its members, or header:<name>.
func renderPage(doc *ir.Document, op ir.Operation, p *ir.Pagination) string {
	if p == nil {
		return "none"
	}
	names := map[ir.PropID]string{}
	for _, td := range doc.Types {
		if m, ok := td.(*ir.Model); ok {
			for _, prop := range m.Properties {
				names[prop.ID] = prop.Name.Source
			}
		}
	}
	for _, r := range op.Responses {
		for _, h := range r.Headers {
			names[h.ID] = h.Name.Source
		}
	}
	path := func(segments []ir.PropID) string {
		words := make([]string, len(segments))
		for i, s := range segments {
			words[i] = names[s]
		}
		return strings.Join(words, ".")
	}
	in := func(name string, v *ir.InputPath) string {
		switch {
		case v == nil:
			return ""
		case v.Param == nil:
			return " " + name + "=body:" + path(v.Segments)
		}
		for _, param := range op.Params {
			if param.ID == *v.Param {
				return " " + name + "=param:" + strings.TrimSuffix(param.Name.Source+"."+path(v.Segments), ".")
			}
		}
		return " " + name + "=?"
	}
	out := func(name string, v *ir.PropPath) string {
		switch {
		case v == nil:
			return ""
		case v.In == "header":
			return " " + name + "=header:" + path(v.Segments)
		default:
			return " " + name + "=" + path(v.Segments)
		}
	}
	return string(p.Strategy) + in("in", p.InputCursor) + in("limit", p.InputLimit) + out("items", p.Items) +
		out("next", p.NextCursor) + out("link", p.NextLink) + out("total", p.TotalCount) + out("pages", p.TotalPages)
}

// TestPromotePagination_FillsWhatTheExtensionsDeclare pins each way a policy's
// sources state a pagination that fills the field: an expression on the
// operation, as a runtime expression or as the dotted shorthand, and a key
// marking a parameter, a member or the type a member names. Each walk's
// strategy is the input that continues it, or its next link.
func TestPromotePagination_FillsWhatTheExtensionsDeclare(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, op, want string
	}{
		{"a cursor walk in shorthand", `      x-page: {cursor: $request.c, next: $response.info.next, items: $response.items, limit: $request.n}
      parameters:
        - {name: c, in: query, schema: {type: string}}
        - {name: n, in: query, schema: {type: integer}}
` + pageResponse, "cursor in=param:c limit=param:n items=items next=info.next"},
		{"in runtime expressions", `      x-page: {cursor: $request.query.c, next: '$response.body#/info/next', total: '$response.body#/info/total'}
      parameters:
        - {name: c, in: query, schema: {type: string}}
` + pageResponse, "cursor in=param:c next=info.next total=info.total"},
		{"an offset walk", `      x-page: {offset: $request.at, items: $response.items, pages: $response.info.total}
      parameters:
        - {name: at, in: query, schema: {type: integer}}
` + pageResponse, "offset in=param:at items=items pages=info.total"},
		{"a page walk, its inputs in the body", `      x-page: {page: '$request.body#/cursor', limit: $request.size, items: $response.items}
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Query'}}}}
` + pageResponse, "page in=body:cursor limit=body:size items=items"},
		{"a member of a parameter", `      x-page: {page: $request.q.cursor, items: $response.items}
      parameters:
        - {name: q, in: query, content: {application/json: {schema: {$ref: '#/components/schemas/Query'}}}}
` + pageResponse, "page in=param:q.cursor items=items"},
		{"a next link", `      x-page: {link: $response.next}
` + pageResponse, "next_link link=next"},
		{"a next link in a header", `      x-page: {link: $response.header.Next}
      responses:
        "200":
          description: ok
          headers: {Next: {schema: {type: string}}}
`, "next_link link=header:Next"},
		{"a page whole", `      x-page: {link: $response.next, items: '$response.body'}
` + pageResponse, "next_link items= link=next"},
		{"marked nodes", `      parameters:
        - {name: c, in: query, schema: {type: string, x-role: {purpose: cursor}}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data: {type: array, items: {type: string}, x-role: {purpose: items}}
                  meta: {$ref: '#/components/schemas/Tokens'}
`, "cursor in=param:c items=data next=meta.token"},
		{"a page marked whole", `      x-page: {link: $response.next}
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Marked'}}}
`, "next_link items= link=next"},
		{"a marked member of the body", `      x-page: {next: $response.next, items: $response.items}
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {after: {type: string, x-role: {purpose: cursor}}}}
` + pageResponse, "cursor in=body:after items=items next=next"},
		{"a marked type a parameter names", `      x-page: {next: $response.next}
      parameters:
        - {name: c, in: query, schema: {$ref: '#/components/schemas/Cursor'}}
` + pageResponse, "cursor in=param:c next=next"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, op, diags := compilePaged(t, tc.op, pagePolicy())
			assert.Equal(t, tc.want, renderPage(doc, op, op.Pagination))
			if op.Pagination != nil {
				assert.True(t, op.Pagination.Inferred, "a pagination an extension declares is still a guess")
				openapitest.RequireNoErrorDiags(t, diags)
			}
		})
	}
}

// TestPromotePagination_ReportsWhatFillsNone pins each pagination the
// extensions state that fills nothing, reported at what stated it: a value
// that resolves to nothing, a mark several nodes answer, a walk no pager can
// advance or end, and a walk with no way to its next page. A pagination is
// filled whole or not at all.
func TestPromotePagination_ReportsWhatFillsNone(t *testing.T) {
	t.Parallel()
	query := func(names ...string) string {
		out := "      parameters:\n"
		for _, n := range names {
			out += "        - {name: " + n + ", in: query, schema: {type: string}}\n"
		}
		return out
	}
	for _, tc := range []struct {
		name, op, want string
	}{
		{"two inputs continue it", "      x-page: {cursor: $request.c, page: $request.p, next: $response.next}\n" +
			query("c", "p") + pageResponse, "names more than one input continuing it (pagination.cursor, pagination.page)"},
		{"a cursor with nothing to send", "      x-page: {cursor: $request.c}\n" + query("c") + pageResponse,
			"sends a cursor and names no next cursor to send"},
		{"a next cursor with nowhere to send it", "      x-page: {next: $response.next}\n" + pageResponse,
			"names a next cursor and no input to send it in"},
		{"an offset with no items to count", "      x-page: {offset: $request.at}\n" + query("at") + pageResponse,
			"counts its walk by pagination.offset and names no items to count"},
		{"a page number with no items to count", "      x-page: {page: $request.p, limit: $request.n}\n" +
			query("p", "n") + pageResponse, "counts its walk by pagination.page and names no items to count"},
		{"nothing reaching a next page", "      x-page: {items: $response.items}\n" + pageResponse,
			"names neither an input continuing it nor a next link"},
		{"a response value read off the request", "      x-page: {cursor: '$response.body#/next'}\n" + pageResponse,
			`"x-page.cursor" names no value of the request`},
		{"a parameter the operation lacks", "      x-page: {cursor: $request.query.c}\n" + pageResponse,
			`reads query parameter "c", which the operation does not declare once`},
		{"no expression", "      x-page: {cursor: c}\n" + pageResponse, "is no expression naming a value of the request"},
		{"an empty name", "      x-page: {cursor: $request.c..d}\n" + pageResponse,
			"is no expression naming a value of the request"},
		{"a body the request lacks", "      x-page: {cursor: $request.after}\n" + pageResponse,
			"reads a request body the document does not describe"},
		{"a name two locations carry", "      x-page: {cursor: $request.c}\n      parameters:\n" +
			"        - {name: c, in: query, schema: {type: string}}\n        - {name: c, in: header, schema: {type: string}}\n" +
			pageResponse, `names parameter "c", which the operation declares in more than one location`},
		{"a chain a parameter does not hold", "      x-page: {cursor: $request.c.d}\n" + query("c") + pageResponse,
			`reads d of parameter "c", which holds no such chain of members`},
		{"a request value read off a page", "      x-page: {link: $request.query.c}\n" + query("c") + pageResponse,
			"names no value of a page"},
		{"a response's query", "      x-page: {link: $response.query.c}\n" + pageResponse,
			"reads a response's query parameter, which no response carries"},
		{"no page expression", "      x-page: {link: next}\n" + pageResponse, "is no expression naming a value of a page"},
		{"a member no page holds", "      x-page: {link: $response.gone}\n" + pageResponse,
			"reads /gone, which the response body's application/json content holds as no chain of members"},
		{"no page body", "      x-page: {link: $response.next}\n      responses: {\"204\": {description: none}}",
			"reads a page body, and no success response declares one"},
		{"a header on no response", "      x-page: {link: $response.header.Next}\n      responses: {default: {description: e}}",
			`reads response header "Next", and the operation declares no success response`},
		{"a header a response lacks", "      x-page: {link: $response.header.Next}\n" + pageResponse,
			`reads response header "Next", which a success response does not declare once`},
		{"a mark on two parameters", "      x-page: {next: $response.next}\n      parameters:\n" +
			"        - {name: a, in: query, schema: {$ref: '#/components/schemas/Cursor'}}\n" +
			"        - {name: b, in: query, schema: {$ref: '#/components/schemas/Cursor'}}\n" + pageResponse,
			"marks more than one value of the request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, op, diags := compilePaged(t, tc.op, pagePolicy())
			assert.Nil(t, op.Pagination)
			var found bool
			for _, d := range diags {
				found = found || strings.Contains(d.Message, tc.want) && d.Code == "openapi/degraded-construct"
			}
			assert.True(t, found, "want a report holding %q, got %+v", tc.want, diags)
		})
	}
}

// twoResponses is a pair of success responses, 200 a Page and 201 body, each
// declaring the header Next.
func twoResponses(body string) string {
	return `      responses:
        "200":
          description: ok
          headers: {Next: {schema: {type: string}}}
          content: {application/json: {schema: {$ref: '#/components/schemas/Page'}}}
        "201":
          description: ok
          headers: {Next: {schema: {type: string}}}
          content: {application/json: {schema: ` + body + `}}`
}

// TestPromotePagination_ReadsEverySuccessResponse pins a page value to the
// success responses and their media types alike: each must hold it at one
// member, a response with no body holding none, and a header no one member of
// all of them. It also pins what sources of one target that state values on
// one operation do: the policy's first decides, and a later one naming another
// value is reported, while one naming the same is not.
func TestPromotePagination_ReadsEverySuccessResponse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, op, want, report string
	}{
		{"two responses holding one member", "      x-page: {link: $response.next}\n" +
			twoResponses(`{$ref: '#/components/schemas/Page'}`), "next_link link=next", ""},
		{"a response with no body", "      x-page: {link: $response.next}\n      responses:\n" +
			"        \"200\": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Page'}}}}\n" +
			"        \"204\": {description: none}", "next_link link=next", ""},
		{"two responses holding two", "      x-page: {link: $response.next}\n" +
			twoResponses(`{type: object, properties: {next: {type: string}}}`), "none",
			"reads next, which the success responses hold as different members"},
		{"a header two responses declare", "      x-page: {link: $response.header.Next}\n" +
			twoResponses(`{$ref: '#/components/schemas/Page'}`), "none",
			`reads response header "Next", which the success responses declare apart`},
		{"two marks in two responses", "      x-page: {cursor: $request.c}\n" + queryC +
			twoResponses(`{$ref: '#/components/schemas/Tokens'}`) + "\n        \"202\":\n          description: ok\n" +
			"          content: {application/json: {schema: {type: object, properties: {t: {$ref: '#/components/schemas/Token'}}}}}",
			"none", "marks different members of the success responses"},
		{"a marked header", "      x-page: {cursor: $request.c}\n" + queryC + "      responses:\n" +
			"        \"200\":\n          description: ok\n" +
			"          headers: {Next: {schema: {$ref: '#/components/schemas/Token'}}}",
			"cursor in=param:c next=header:Next", ""},
		{"a marked header beside a marked member", "      x-page: {cursor: $request.c}\n" + queryC + "      responses:\n" +
			"        \"200\":\n          description: ok\n" +
			"          headers: {Next: {schema: {$ref: '#/components/schemas/Token'}}}\n" +
			"          content: {application/json: {schema: {$ref: '#/components/schemas/Tokens'}}}",
			"none", "marks more than one value of a response"},
		{"a body marking two members", "      x-page: {cursor: $request.c}\n" + queryC + "      responses:\n" +
			"        \"200\":\n          description: ok\n          content:\n            application/json:\n" +
			"              schema: {type: object, properties: {a: {$ref: '#/components/schemas/Token'}, " +
			"b: {$ref: '#/components/schemas/Token'}}}", "none", "marks more than one member of a body, as near its root"},
		{"one mark in two responses", "      x-page: {cursor: $request.c}\n" + queryC + "      responses:\n" +
			"        \"200\": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Tokens'}}}}\n" +
			"        \"201\": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Tokens'}}}}",
			"cursor in=param:c next=token", ""},
		{"two media types marking two members", "      x-page: {cursor: $request.c}\n" + queryC + "      responses:\n" +
			"        \"200\":\n          description: ok\n          content:\n" +
			"            application/json: {schema: {$ref: '#/components/schemas/Tokens'}}\n" +
			"            application/xml: {schema: {type: object, properties: {t: {$ref: '#/components/schemas/Token'}}}}",
			"none", "marks different members of a body's media types"},
		{"a model reached twice as near", "      x-page: {cursor: $request.c}\n" + queryC + "      responses:\n" +
			"        \"200\":\n          description: ok\n          content:\n            application/json:\n" +
			"              schema: {type: object, properties: {a: {$ref: '#/components/schemas/Tokens'}, " +
			"b: {$ref: '#/components/schemas/Tokens'}}}", "none", "marks more than one member of a body, as near its root"},
		{"two marked members of a request body", "      x-page: {next: $response.next}\n      requestBody:\n" +
			"        content: {application/json: {schema: {type: object, properties: {a: {$ref: '#/components/schemas/Cursor'}, " +
			"b: {$ref: '#/components/schemas/Cursor'}}}}}\n" + pageResponse, "none",
			"marks more than one member of a body, as near its root"},
		{"an expression and a mark naming one page value", "      x-page: {cursor: $request.c, next: $response.token}\n" +
			queryC + "      responses:\n" +
			"        \"200\": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Tokens'}}}}",
			"cursor in=param:c next=token", ""},
		{"two sources naming one value", "      x-page: {cursor: $request.c, next: $response.next}\n      parameters:\n" +
			"        - {name: c, in: query, schema: {$ref: '#/components/schemas/Cursor'}}\n" + pageResponse,
			"cursor in=param:c next=next", ""},
		{"two sources naming two", "      x-page: {cursor: $request.c, next: $response.next}\n      parameters:\n" +
			"        - {name: c, in: query, schema: {type: string}}\n" +
			"        - {name: d, in: query, schema: {$ref: '#/components/schemas/Cursor'}}\n" + pageResponse,
			"cursor in=param:c next=next", `extension "x-role.purpose" states another pagination.cursor than "x-page.cursor"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, op, diags := compilePaged(t, tc.op, pagePolicy())
			assert.Equal(t, tc.want, renderPage(doc, op, op.Pagination))
			var reports []string
			for _, d := range diags {
				if d.Code == "openapi/degraded-construct" {
					reports = append(reports, d.Message)
				}
			}
			if tc.report == "" {
				assert.Empty(t, reports)
				return
			}
			require.Len(t, reports, 1)
			assert.Contains(t, reports[0], tc.report)
		})
	}
}

// queryC declares one string query parameter, c.
const queryC = "      parameters:\n        - {name: c, in: query, schema: {type: string}}\n"

// TestPromotePagination_ReadsNothingOffPolicy pins the walk to a policy that
// maps a pagination target: under one mapping none, and under the policy off,
// a document declaring one fills nothing and reports nothing.
func TestPromotePagination_ReadsNothingOffPolicy(t *testing.T) {
	t.Parallel()
	op := "      x-page: {link: $response.next}\n" + pageResponse
	for name, policy := range map[string]openapi.ExtensionPromotions{
		"no pagination target": {Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
			openapi.TargetDeprecationMessage: {{Key: "x-why"}},
		}},
		"off": {Disabled: true},
	} {
		_, got, diags := compilePaged(t, op, policy)
		assert.Nil(t, got.Pagination, name)
		assert.Empty(t, diags, name)
	}
}
