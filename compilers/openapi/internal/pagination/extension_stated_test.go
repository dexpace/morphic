// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/pagination"
	"github.com/dexpace/morphic/ir"
)

// statedPaths declares one operation per path, each named by its operationId
// for the way its extensions speak of a pagination under pagePolicy, or do not.
var statedPaths = []string{`  /filled:
    get:
      operationId: filled
      x-page: {link: $response.next}
` + pageResponse, `  /unresolved:
    get:
      operationId: unresolved
      x-page: {cursor: $request.query.c, next: $response.next}
` + pageResponse, `  /refused:
    get:
      operationId: refused
      x-page: {cursor: $request.c}
      parameters: [{name: c, in: query, schema: {type: string}}]
` + pageResponse, `  /false:
    get:
      operationId: statedFalse
      x-page: false
` + pageResponse, `  /null:
    get:
      operationId: statedNull
      x-page: null
` + pageResponse, `  /empty:
    get:
      operationId: statedEmpty
      x-page: {}
` + pageResponse, `  /parameter:
    get:
      operationId: markedParameter
      parameters: [{name: q, in: query, schema: {type: string, x-role: {purpose: other}}}]
` + pageResponse, `  /member:
    get:
      operationId: markedMember
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {type: object, properties: {a: {type: string, x-role: false}}}}}`,
	`  /header:
    get:
      operationId: markedHeader
      responses:
        "200":
          description: ok
          headers: {Next: {schema: {type: string, x-role: {}}}}`, `  /type:
    get:
      operationId: markedType
      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/Unsaid'}}]
` + pageResponse, `  /shared/a:
    get:
      operationId: sharedA
      responses: {"200": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Listing'}}}}}`,
	`  /shared/b:
    get:
      operationId: sharedB
      responses: {"200": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Listing'}}}}}`,
	`  /silent:
    get:
      operationId: silent
` + pageResponse, `  /unmapped:
    get:
      operationId: unmapped
      x-pages: {link: $response.next}
` + pageResponse, `  /onTheOperation:
    get:
      operationId: markOnTheOperation
      x-role: {purpose: items}
` + pageResponse, `  /failure:
    get:
      operationId: markOnAFailure
      responses:
        "400":
          description: bad
          content: {application/json: {schema: {type: object, properties: {a: {type: string, x-role: {purpose: next}}}}}}`,
	`  /stream:
    get:
      operationId: markInAStream
      responses:
        "200":
          description: ok
          content: {text/event-stream: {schema: {type: object, properties: {a: {type: string, x-role: {purpose: next}}}}}}`,
}

// statedSpec is a document declaring paths, in that order, over pagedSpec's
// components and three of its own.
func statedSpec(t *testing.T, paths []string) string {
	t.Helper()
	_, components, found := strings.Cut(pagedSpec(""), "\ncomponents:\n")
	require.True(t, found, "pagedSpec declares components")
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + strings.Join(paths, "\n") + "\ncomponents:\n" + components +
		`    Unsaid: {type: string, x-role: null}
    Listing: {type: object, properties: {after: {$ref: '#/components/schemas/Shared'}}}
    Shared: {type: string, x-role: {purpose: next}}
`
}

// TestPromote_SaysWhichOperationsSpeakOfAPagination pins the
// operations Promote returns as stated: each whose extensions write
// a key the policy maps to a pagination target where that key is read, filled,
// failing to resolve, refused or stating nothing, on the operation or by a mark
// on a parameter, a member, a header or a type, a shared one marking each
// operation reaching it. An operation writing no such key, or one only where
// it is not read, is not stated, and nothing it did not fill is marked.
func TestPromote_SaysWhichOperationsSpeakOfAPagination(t *testing.T) {
	t.Parallel()
	spoke, ops, _ := promoteStated(t, statedSpec(t, statedPaths), pagePolicy())
	assert.Equal(t, []string{
		"filled", "markedHeader", "markedMember", "markedParameter", "markedType", "refused",
		"sharedA", "sharedB", "statedEmpty", "statedFalse", "statedNull", "unresolved",
	}, spoke)

	require.NotNil(t, ops["filled"].Pagination, "a stated pagination that resolves still fills")
	assert.Contains(t, ops["filled"].Provenance.Inferred, lowering.ExtensionPromotionHeuristic)
	for name, op := range ops {
		if name == "filled" {
			continue
		}
		assert.Nil(t, op.Pagination, name)
		assert.NotContains(t, op.Provenance.Inferred, lowering.ExtensionPromotionHeuristic,
			"%s: nothing filled, so nothing is marked", name)
	}
}

// keyPosition is a place a pagination key is read: the operation writing ops,
// in which V stands for the key's value, and the components it adds, the
// pointer the key sits at, and whether it is the expression key or a mark, and
// one a page reads.
type keyPosition struct {
	name, op, components, at string
	mark, page               bool
}

// keyPositions are the operation, read for an expression, and every node a
// search for a mark reads. Each writes no other key, so whatever the step
// reports is about the value written at it.
func keyPositions() []keyPosition {
	const (
		bare   = "      responses: {\"200\": {description: ok}}"
		paged  = "      responses: {\"200\": {description: ok, content: {application/json: {schema: %s}}}}"
		posted = "      requestBody: {content: {application/json: {schema: %s}}}\n" + bare
		role   = "    Role: {type: string, x-role: V}\n"
		op     = "/paths/~1x/get"
	)
	return []keyPosition{
		{name: "the operation", op: "      x-page: V\n" + pageResponse, at: op + "/x-page"},
		{name: "a parameter", op: "      parameters: [{name: q, in: query, x-role: V, schema: {type: string}}]\n" + bare,
			at: op + "/parameters/0/x-role", mark: true},
		{name: "a parameter's schema", op: "      parameters: [{name: q, in: query, schema: {type: string, x-role: V}}]\n" + bare,
			at: op + "/parameters/0/schema/x-role", mark: true},
		{name: "a parameter's type", op: "      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/Role'}}]\n" + bare,
			components: role, at: "/components/schemas/Role/x-role", mark: true},
		{name: "a member of the request", op: fmt.Sprintf(posted, "{type: object, properties: {a: {type: string, x-role: V}}}"),
			at: op + "/requestBody/content/application~1json/schema/properties/a/x-role", mark: true},
		{name: "the request whole", op: fmt.Sprintf(posted, "{type: object, x-role: V, properties: {a: {type: string}}}"),
			at: op + "/requestBody/content/application~1json/schema/x-role", mark: true},
		{name: "a member of a page", op: fmt.Sprintf(paged, "{type: object, properties: {a: {type: string, x-role: V}}}"),
			at: op + "/responses/200/content/application~1json/schema/properties/a/x-role", mark: true, page: true},
		{name: "a page whole", op: fmt.Sprintf(paged, "{type: object, x-role: V, properties: {a: {type: string}}}"),
			at: op + "/responses/200/content/application~1json/schema/x-role", mark: true, page: true},
		{name: "a type a member of a page names", op: fmt.Sprintf(paged, "{type: object, properties: {a: {$ref: '#/components/schemas/Role'}}}"),
			components: role, at: "/components/schemas/Role/x-role", mark: true, page: true},
		{name: "a header", op: "      responses: {\"200\": {description: ok, headers: {Next: {x-role: V, schema: {type: string}}}}}",
			at: op + "/responses/200/headers/Next/x-role", mark: true, page: true},
	}
}

// keyValue is a value a pagination key may hold, and what it is reported as, a
// mark's and an expression's: empty for nothing, or the report's text before
// the clause naming what it keeps off, at the member it reads where atMember.
type keyValue struct {
	name, value, mark, expression string
	atMember                      bool
}

// TestPromote_ReportsEachValueAKeyHolds pins what each value a
// pagination key may hold states, at each position a key is read, through the
// step: false, and a mark's word another source lists, report nothing; true,
// null, a scalar, a list, an object holding no member a source reads, an
// expression that is no string and a word no source lists are each reported
// once, at the key or its member, saying the key, being written, keeps an
// inferred pagination off. Each speaks, so the operation is stated, and none is
// filled or marked.
func TestPromote_ReportsEachValueAKeyHolds(t *testing.T) {
	t.Parallel()
	const noWalk = ", so it states no value of a walk"
	var (
		noMarkMember = `extension "x-role" holds none of the members the policy reads (purpose)` + noWalk
		noExpression = `extension "x-page" holds none of the members the policy reads ` +
			`(cursor, items, limit, link, next, offset, page, pages, total)` + noWalk
	)
	shapes := []struct{ name, value string }{
		{"true", "true"}, {"null", "null"}, {"a string", "after"}, {"a number", "3"}, {"a list", "[after]"},
	}
	values := make([]keyValue, 0, 5+len(shapes))
	values = append(values,
		keyValue{name: "false", value: "false"},
		keyValue{name: "a word another source lists", value: "{purpose: OTHER}", expression: noExpression},
		keyValue{name: "an empty object", value: "{}", mark: noMarkMember, expression: noExpression},
		keyValue{name: "an object of another member", value: "{other: 1}", mark: noMarkMember, expression: noExpression},
		keyValue{name: "a member that names nothing", value: "{purpose: after, cursor: 3}", atMember: true,
			mark:       `extension "x-role.purpose" holds none of the words the policy lists (cursor, items, limit, next)` + noWalk,
			expression: `extension "x-page.cursor" is a number, not a string, so it states no pagination.cursor`},
	)
	for _, shape := range shapes {
		values = append(values, keyValue{name: shape.name, value: shape.value,
			mark:       `extension "x-role" is ` + shape.name + `, not an object` + noWalk,
			expression: `extension "x-page" is ` + shape.name + `, not an object` + noWalk})
	}

	for _, at := range keyPositions() {
		for _, v := range values {
			t.Run(at.name+", "+v.name, func(t *testing.T) {
				t.Parallel()
				other := "next"
				if at.page {
					other = "cursor"
				}
				spec := strings.ReplaceAll(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: x
`+at.op+"\ncomponents:\n  schemas:\n"+at.components+`    Page: {type: object, properties: {next: {type: string}}}
`, "V", strings.ReplaceAll(v.value, "OTHER", other))
				spoke, ops, reports := promoteStated(t, spec, pagePolicy())
				assert.Equal(t, []string{"x"}, spoke, "a key written speaks, whatever it holds")
				assert.Nil(t, ops["x"].Pagination)
				assert.NotContains(t, ops["x"].Provenance.Inferred, lowering.ExtensionPromotionHeuristic)

				want, pointer := v.expression, at.at
				reach := ", and as the key is written, no pagination is inferred for the operation"
				if at.mark {
					want, reach = v.mark, ", and as the key is written, no pagination is inferred for an operation reaching it"
				}
				switch {
				case want == "":
					assert.Empty(t, reports)
					return
				case v.atMember && at.mark:
					pointer += "/purpose"
				case v.atMember:
					pointer += "/cursor"
				}
				assert.Equal(t, []string{pointer + ": " + want + reach}, reports)
			})
		}
	}
}

// promoteStated compiles spec with promotion off, so each extension is kept and
// no pagination is filled, then promotes its pagination under policy. It
// returns the operationIds Promote says spoke, the operations by
// operationId, and its reports, each once, as its pointer and its message,
// since a compile drops the repeats of one report the sources of a key make.
func promoteStated(t *testing.T, spec string, policy lowering.ExtensionPromotions) ([]string, map[string]*ir.Operation, []string) {
	t.Helper()
	doc, _, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(spec)},
		compilers.Options{FormatOptions: openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Promotions: policy})
	stated, diags := pagination.Promote(c, lowering.NewWhole(doc))
	reports := make([]string, 0, len(diags))
	for _, d := range diags {
		require.Equal(t, ir.SeverityInfo, d.Severity, "%+v", d)
		require.Equal(t, "openapi/degraded-construct", d.Code, "%+v", d)
		reports = append(reports, string(d.Provenance.Pointer)+": "+d.Message)
	}
	slices.Sort(reports)
	ops := map[string]*ir.Operation{}
	var spoke []string
	for _, op := range lowering.NewWhole(doc).Ops {
		ops[op.Name.Source] = op
		if stated[op.ID] {
			spoke = append(spoke, op.Name.Source)
		}
	}
	slices.Sort(spoke)
	return spoke, ops, slices.Compact(reports)
}

// TestPromote_StatedIsIndependentOfDeclarationOrder pins the stated
// operations to the document, not the order it declares them in: a shared
// type's reach is read once, by whichever operation comes first.
func TestPromote_StatedIsIndependentOfDeclarationOrder(t *testing.T) {
	t.Parallel()
	backward := slices.Clone(statedPaths)
	slices.Reverse(backward)
	forward, _, _ := promoteStated(t, statedSpec(t, statedPaths), pagePolicy())
	reversed, _, _ := promoteStated(t, statedSpec(t, backward), pagePolicy())
	require.NotEmpty(t, forward)
	assert.Equal(t, forward, reversed)
}

// TestPromote_StatedReadsEitherSidesKey pins a key a policy reads for
// a request target alone, and one it reads for a page value alone, each to an
// operation it states though it states nothing.
func TestPromote_StatedReadsEitherSidesKey(t *testing.T) {
	t.Parallel()
	policy := lowering.ExtensionPromotions{Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
		lowering.TargetPaginationCursor:   {{Key: "x-in", Member: "cursor"}},
		lowering.TargetPaginationNextLink: {{Key: "x-out", Member: "link"}},
	}}
	spoke, _, _ := promoteStated(t, statedSpec(t, []string{`  /in:
    get:
      operationId: requestSide
      x-in: false
` + pageResponse, `  /out:
    get:
      operationId: pageSide
      x-out: {}
` + pageResponse, `  /neither:
    get:
      operationId: neither
      x-page: {link: $response.next}
` + pageResponse}), policy)
	assert.Equal(t, []string{"pageSide", "requestSide"}, spoke)
}

// TestPromote_ReadsNoOperationHoldingOne pins an operation holding a
// pagination when the promotion runs to one it neither reads nor states: its
// pagination is left as it is, and a consumer reads that field instead.
func TestPromote_ReadsNoOperationHoldingOne(t *testing.T) {
	t.Parallel()
	doc, op, _ := compilePaged(t, "      x-page: {link: $response.next}\n"+pageResponse, pagePolicy())
	require.NotNil(t, op.Pagination)
	held := doc.Services[0].Groups[0].Operations[0].Pagination
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Promotions: pagePolicy()})
	stated, diags := pagination.Promote(c, lowering.NewWhole(doc))
	assert.Empty(t, stated)
	assert.Empty(t, diags)
	assert.Same(t, held, doc.Services[0].Groups[0].Operations[0].Pagination)
}

// TestPromote_StatesNothingOffPolicy pins the stated operations to a
// policy mapping a pagination target: one mapping none, or the policy off,
// reads no key, so no operation is stated, whatever its keys.
func TestPromote_StatesNothingOffPolicy(t *testing.T) {
	t.Parallel()
	for name, policy := range map[string]lowering.ExtensionPromotions{
		"no pagination target": {Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
			lowering.TargetDeprecationMessage: {{Key: "x-page"}},
		}},
		"off": {Disabled: true},
	} {
		spoke, _, _ := promoteStated(t, statedSpec(t, statedPaths), policy)
		assert.Empty(t, spoke, name)
	}
	stated, diags := pagination.Promote(lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{}), nil)
	assert.Nil(t, stated, "no service, nothing read")
	assert.Empty(t, diags)
}
