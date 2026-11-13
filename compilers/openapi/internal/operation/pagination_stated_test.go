// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
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
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
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

// promoteStated compiles spec with promotion off, so each extension is kept and
// no pagination is filled, then promotes its pagination under policy. It
// returns the operationIds PromotePagination says spoke and the operations by
// operationId.
func promoteStated(t *testing.T, spec string, policy lowering.ExtensionPromotions) ([]string, map[string]*ir.Operation) {
	t.Helper()
	doc, _, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(spec)},
		compilers.Options{FormatOptions: openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Promotions: policy})
	stated, _ := operation.PromotePagination(c, &doc.Services[0], doc.Types)
	ops := map[string]*ir.Operation{}
	var spoke []string
	for _, op := range lowering.Operations(&doc.Services[0]) {
		ops[op.Name.Source] = op
		if stated[op.ID] {
			spoke = append(spoke, op.Name.Source)
		}
	}
	slices.Sort(spoke)
	return spoke, ops
}

// TestPromotePagination_SaysWhichOperationsSpeakOfAPagination pins the
// operations PromotePagination returns as stated: each whose extensions write
// a key the policy maps to a pagination target where that key is read, filled,
// failing to resolve, refused or stating nothing, on the operation or by a mark
// on a parameter, a member, a header or a type, a shared one marking each
// operation reaching it. An operation writing no such key, or one only where
// it is not read, is not stated, and nothing it did not fill is marked.
func TestPromotePagination_SaysWhichOperationsSpeakOfAPagination(t *testing.T) {
	t.Parallel()
	spoke, ops := promoteStated(t, statedSpec(t, statedPaths), pagePolicy())
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

// TestPromotePagination_StatedIsIndependentOfDeclarationOrder pins the stated
// operations to the document, not the order it declares them in: a shared
// type's reach is read once, by whichever operation comes first.
func TestPromotePagination_StatedIsIndependentOfDeclarationOrder(t *testing.T) {
	t.Parallel()
	backward := slices.Clone(statedPaths)
	slices.Reverse(backward)
	forward, _ := promoteStated(t, statedSpec(t, statedPaths), pagePolicy())
	reversed, _ := promoteStated(t, statedSpec(t, backward), pagePolicy())
	require.NotEmpty(t, forward)
	assert.Equal(t, forward, reversed)
}

// TestPromotePagination_StatedReadsEitherSidesKey pins a key a policy reads for
// a request target alone, and one it reads for a page value alone, each to an
// operation it states though it states nothing.
func TestPromotePagination_StatedReadsEitherSidesKey(t *testing.T) {
	t.Parallel()
	policy := lowering.ExtensionPromotions{Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
		lowering.TargetPaginationCursor:   {{Key: "x-in", Member: "cursor"}},
		lowering.TargetPaginationNextLink: {{Key: "x-out", Member: "link"}},
	}}
	spoke, _ := promoteStated(t, statedSpec(t, []string{`  /in:
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

// TestPromotePagination_ReadsNoOperationHoldingOne pins an operation holding a
// pagination when the promotion runs to one it neither reads nor states: its
// pagination is left as it is, and a consumer reads that field instead.
func TestPromotePagination_ReadsNoOperationHoldingOne(t *testing.T) {
	t.Parallel()
	doc, op, _ := compilePaged(t, "      x-page: {link: $response.next}\n"+pageResponse, pagePolicy())
	require.NotNil(t, op.Pagination)
	held := doc.Services[0].Groups[0].Operations[0].Pagination
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Promotions: pagePolicy()})
	stated, diags := operation.PromotePagination(c, &doc.Services[0], doc.Types)
	assert.Empty(t, stated)
	assert.Empty(t, diags)
	assert.Same(t, held, doc.Services[0].Groups[0].Operations[0].Pagination)
}

// TestPromotePagination_StatesNothingOffPolicy pins the stated operations to a
// policy mapping a pagination target: one mapping none, or the policy off,
// reads no key, so no operation is stated, whatever its keys.
func TestPromotePagination_StatesNothingOffPolicy(t *testing.T) {
	t.Parallel()
	for name, policy := range map[string]lowering.ExtensionPromotions{
		"no pagination target": {Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
			lowering.TargetDeprecationMessage: {{Key: "x-page"}},
		}},
		"off": {Disabled: true},
	} {
		spoke, _ := promoteStated(t, statedSpec(t, statedPaths), policy)
		assert.Empty(t, spoke, name)
	}
	stated, diags := operation.PromotePagination(lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{}),
		nil, ir.TypeRegistry{})
	assert.Nil(t, stated, "no service, nothing read")
	assert.Empty(t, diags)
}
