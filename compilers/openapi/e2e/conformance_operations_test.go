// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertTagsGrouping(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	assert.Equal(t, "pets", doc.Services[0].Groups[0].Name.Source)
	require.Len(t, doc.TagDefs, 1)
	assert.Equal(t, "pets", doc.TagDefs[0].Name)
}

// assertGroupIdentity pins what identifies a group: the rule that formed it and
// the key that rule read, never its name. Two pairs among the seven render from
// the same words — the declared tag "default" and the group untagged operations
// fall into, the tag "webhooks" and the webhook group — and two tags differ only
// by what RFC 6901 escapes. Each keeps an ID of its own. The IDs are written out
// rather than derived, so the test cannot agree with a change to the derivation
// (GitHub #673).
func assertGroupIdentity(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	got := map[ir.GroupID][]string{}
	for _, operationGroup := range doc.Services[0].Groups {
		require.NotContains(t, got, operationGroup.ID, "two groups share an ID")
		for _, operation := range operationGroup.Operations {
			got[operationGroup.ID] = append(got[operationGroup.ID], operation.Name.Source)
		}
	}
	assert.Equal(t, map[ir.GroupID][]string{
		"operation-group/tags/default":  {"declared"},
		"operation-group/default":       {"untagged"},
		"operation-group/tags/webhooks": {"namedLikeWebhooks"},
		"operation-group/tags/ghost":    {"undeclared"},
		"operation-group/tags/a~1b":     {"slash"},
		"operation-group/tags/a~01b":    {"tilde"},
		"operation-group/webhooks":      {"onEvent"},
	}, got)
}

// assertParamIdentity pins what identifies a parameter: the operation it is
// mounted under, its name as one escaped token, and its location. Two
// parameters named id in one operation get two IDs; a path-item parameter is one
// parameter per operation it is copied into; a path item reached from two paths
// roots each mount's parameters at that mount. The IDs are written out rather
// than derived, so the test cannot agree with a change to the derivation, and
// every binding must name its own parameter's ID.
func assertParamIdentity(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	got := map[ir.OperationID][]ir.ParameterID{}
	svc := doc.Services[0]
	for _, operationGroup := range svc.Groups {
		for _, operation := range operationGroup.Operations {
			for _, parameter := range operation.Params {
				got[operation.ID] = append(got[operation.ID], parameter.ID)
			}
			require.Len(t, operation.Bindings.HTTP, 1)
			var bound []ir.ParameterID
			for _, pb := range operation.Bindings.HTTP[0].ParamBindings {
				bound = append(bound, pb.Param)
			}
			assert.ElementsMatch(t, got[operation.ID], bound, "%s: each binding names its own parameter", operation.ID)
		}
	}
	assert.Equal(t, map[ir.OperationID][]ir.ParameterID{
		"operation/openapi/paths/~1items~1{id}/get": {
			"parameter/openapi/paths/~1items~1{id}/get/parameters/id/query",
			"parameter/openapi/paths/~1items~1{id}/get/parameters/a~1b/query",
			"parameter/openapi/paths/~1items~1{id}/get/parameters/id/path",
		},
		"operation/openapi/paths/~1items~1{id}/post": {
			"parameter/openapi/paths/~1items~1{id}/post/parameters/id/path",
		},
		"operation/openapi/paths/~1a/get": {"parameter/openapi/paths/~1a/get/parameters/limit/query"},
		"operation/openapi/paths/~1b/get": {"parameter/openapi/paths/~1b/get/parameters/limit/query"},
	}, got)
}

// assertEnumMemberIdentity pins what identifies an enum member: the value it
// holds, tagged by kind, under the enum that declares it. The list repeats a
// value, includes names that spell one canonical word sequence, and carries the
// characters a pointer token escapes, so a derivation from the name, the
// position or the unescaped value changes an ID. The IDs are written out, not
// derived. The repeat is kept, its ID ends in its occurrence number, and it is
// reported once at its own entry; the string spelling that ID's tail is no
// repeat.
func assertEnumMemberIdentity(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	memberIDs := func(id ir.TypeID) []ir.EnumMemberID {
		enum, ok := doc.Types[id].(*ir.Enum)
		require.True(t, ok, "%s is an enum", id)
		got := make([]ir.EnumMemberID, 0, len(enum.Members))
		for _, enumMember := range enum.Members {
			got = append(got, enumMember.ID)
		}
		return got
	}
	assert.Equal(t, []ir.EnumMemberID{
		"enum-member/openapi/components/schemas/E/string:a-b",
		"enum-member/openapi/components/schemas/E/string:a_b",
		"enum-member/openapi/components/schemas/E/string:A B",
		"enum-member/openapi/components/schemas/E/string:a~0b",
		"enum-member/openapi/components/schemas/E/string:a-b/2",
		"enum-member/openapi/components/schemas/E/string:a-b~12",
	}, memberIDs("type/openapi/components/schemas/E"))
	assert.Equal(t, []ir.EnumMemberID{
		"enum-member/openapi/components/schemas/N/number:-1e0",
		"enum-member/openapi/components/schemas/N/number:1e0",
		"enum-member/openapi/components/schemas/N/number:0",
	}, memberIDs("type/openapi/components/schemas/N"))
	assert.Equal(t, []ir.EnumMemberID{
		"enum-member/anonymous/paths/~1status/get/responses/200/content/application~1json/schema/string:open",
		"enum-member/anonymous/paths/~1status/get/responses/200/content/application~1json/schema/string:closed",
	}, memberIDs("type/anonymous/paths/~1status/get/responses/200/content/application~1json/schema"))

	var repeats []string
	for _, diagnostic := range diags {
		if diagnostic.Code == "openapi/duplicate-enum-value" {
			repeats = append(repeats, string(diagnostic.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/components/schemas/E/enum/4"}, repeats)
}

func assertHTTPBinding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	operation, ok := operationByName(doc, "getItem")
	require.True(t, ok)
	require.Len(t, operation.Bindings.HTTP, 1)
	hb := operation.Bindings.HTTP[0]
	assert.Equal(t, "GET", hb.Method)
	assert.Equal(t, "/items/{id}", hb.URITemplate)
}

// assertPathKeyFragments pins a path key carrying a fragment: the URI template
// is the route a request is sent to, the binding keeps the fragment, and every
// operation sending one route another does shares it, the one written without
// a fragment included. Each fragmented key draws its warning.
func assertPathKeyFragments(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	for name, want := range map[string]struct {
		fragment string
		shared   bool
	}{
		"bearerOAuth": {"oauth2", true}, "bearerApiKey": {"apiKey", true}, "bearer": {"", true}, "other": {"", false},
	} {
		operation, ok := operationByName(doc, name)
		require.True(t, ok, name)
		hb := operation.Bindings.HTTP[0]
		assert.Equal(t, want.shared, hb.SharedRoute, "%s: whether its route is shared", name)
		kept, fragmented := hb.Unmodeled["openapi:fragment"]
		assert.Equal(t, want.fragment != "", fragmented, "%s: whether it keeps a fragment", name)
		if fragmented {
			assert.Equal(t, "/bearer", hb.URITemplate, name)
			assert.JSONEq(t, `"`+want.fragment+`"`, string(kept.Value), name)
		}
	}
	var warned []string
	for _, diagnostic := range diags {
		if diagnostic.Code == "openapi/invalid-path-key" {
			warned = append(warned, string(diagnostic.Provenance.Pointer))
		}
	}
	assert.ElementsMatch(t, []string{"/paths/~1bearer#oauth2", "/paths/~1bearer#apiKey"}, warned)
}

// assertTagsGrouping32 is the 3.2 counterpart of assertTagsGrouping, and the
// first case in the corpus to witness OperationGroup.Groups: a tag declaring a
// parent nests its group under that parent's, an operation's non-navigational
// tag does not become the group it is filed under, and the declared 3.2 tag
// metadata reaches the registry verbatim.
func assertTagsGrouping32(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.TagDefs, 4)
	require.Equal(t, "books", doc.TagDefs[0].Name, "TagDefs keep the document's declaration order")
	assert.Equal(t, "catalog", doc.TagDefs[0].Parent)
	assert.Equal(t, "nav", doc.TagDefs[0].Kind)
	assert.Equal(t, "nav", doc.TagDefs[1].Kind)
	assert.Equal(t, "badge", doc.TagDefs[2].Kind, "the non-navigational kind is recorded, not interpreted")
	assert.Empty(t, doc.TagDefs[2].Parent)

	require.Len(t, doc.Services, 1)
	svc := doc.Services[0]
	require.Len(t, svc.Groups, 1, "the child nests rather than becoming a second top-level group")
	catalog := svc.Groups[0]
	assert.Equal(t, "catalog", catalog.Name.Source)
	assert.Equal(t, "Everything the library holds", catalog.Docs.Description,
		"an ancestor created first on its child's behalf still carries its declared docs")
	require.Len(t, catalog.Operations, 1)
	assert.Equal(t, "showCatalog", catalog.Operations[0].Name.Source)

	require.Len(t, catalog.Groups, 2, "both children nest, in the order operations first reach them")
	assert.Equal(t, "magazines", catalog.Groups[1].Name.Source)
	books := catalog.Groups[0]
	assert.Equal(t, "books", books.Name.Source)
	assert.Equal(t, "Book operations", books.Docs.Description)
	require.Len(t, books.Operations, 1)
	listBooks := books.Operations[0]
	assert.Equal(t, "listBooks", listBooks.Name.Source)
	assert.Equal(t, []string{"beta", "books"}, listBooks.Tags,
		"tag membership keeps every tag the operation named, badge included")
}

// assertRepeatedTagsAndScopes pins that a tag an operation lists twice and a
// scope a requirement lists twice for one scheme are each kept once, at the
// first occurrence, and reported once each, as a warning at the repeat.
func assertRepeatedTagsAndScopes(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	operation, ok := operationByName(doc, "listPets")
	require.True(t, ok)
	assert.Equal(t, []string{"pets", "store"}, operation.Tags)
	require.Len(t, operation.Auth, 1)
	assert.Equal(t, []ir.SchemeUse{{Scheme: "auth/openapi/components/securitySchemes/oauth", Scopes: []string{"read", "write"}}},
		operation.Auth[0].Schemes)

	for _, pointer := range []string{"/paths/~1pets/get/tags/2", "/paths/~1pets/get/security/0/oauth/2"} {
		assert.Equal(t, []ir.Severity{ir.SeverityWarning}, diagsAt(diags, diag.RepeatedEntry, pointer), pointer)
	}
	assert.Len(t, diags, 2, "the two repeats and nothing else: %+v", diags)
}

// assertPaginationInferred pins the walk the pagination-inference policy reads
// off each operation's names (docs/compilers/openapi.md#pagination-policy): a
// page number, an offset, a cursor and a token, each with the page size beside
// it, the items where the body holds them, a cursor walk's next cursor and the
// totals a page states, every one marked inferred; and the operation naming
// two continuations left with none and reported.
func assertPaginationInferred(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	paths := func(segments ...ir.PropertyID) *ir.PropertyPath { return &ir.PropertyPath{Segments: segments} }
	input := func(operation ir.Operation, name string) *ir.InputPath {
		id := linkParam(t, operation, name)
		return &ir.InputPath{Param: &id}
	}
	zones, logs, keys, jobs := linkOperation(t, doc, "listZones"), linkOperation(t, doc, "listLogs"), linkOperation(t, doc, "listKeys"),
		linkOperation(t, doc, "listJobs")
	keysBody := ir.PropertyID("property/openapi/paths/~1keys/get/responses/200/content/application~1json/schema/properties/")
	for operation, want := range map[*ir.Operation]*ir.Pagination{
		&zones: {Strategy: ir.PageStrategyPage, Inferred: true, InputCursor: input(zones, "page"),
			InputLimit: input(zones, "per_page"),
			Items:      paths("property/openapi/paths/~1zones/get/responses/200/content/application~1json/schema/allOf/1/properties/result"),
			TotalCount: paths(componentProperty("Collection/properties/result_info"), componentProperty("ResultInfo/properties/total_count")),
			TotalPages: paths(componentProperty("Collection/properties/result_info"), componentProperty("ResultInfo/properties/total_pages"))},
		&logs: {Strategy: ir.PageStrategyOffset, Inferred: true, InputCursor: input(logs, "offset"),
			InputLimit: input(logs, "limit"), Items: paths()},
		&keys: {Strategy: ir.PageStrategyCursor, Inferred: true, InputCursor: input(keys, "cursor"),
			InputLimit: input(keys, "limit"), Items: paths(keysBody + "result"),
			NextCursor: paths(keysBody+"result_info", componentProperty("CursorInfo/properties/cursor")),
			Stop:       []ir.PageStop{ir.PageStopNoNext}},
		&jobs: {Strategy: ir.PageStrategyToken, Inferred: true, InputCursor: input(jobs, "pageToken"),
			InputLimit: input(jobs, "pageSize"),
			Items:      paths(componentProperty("JobPage/properties/result"), componentProperty("JobPage/properties/result/properties/jobs")),
			NextCursor: paths(componentProperty("JobPage/properties/result"),
				componentProperty("JobPage/properties/result/properties/nextPageToken")),
			Stop: []ir.PageStop{ir.PageStopNoNext}},
	} {
		assert.Equal(t, want, operation.Pagination, operation.Name.Source)
		assert.Equal(t, openapi.PaginationInferenceHeuristic, operation.Provenance.Inferred, operation.Name.Source)
	}

	events := linkOperation(t, doc, "listEvents")
	assert.Nil(t, events.Pagination, "two continuations settle no walk")
	assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/undecided-pagination", "/paths/~1events/get"))
	reported := 0
	for _, diagnostic := range diags {
		if diagnostic.Code == "openapi/undecided-pagination" {
			reported++
		}
	}
	assert.Equal(t, 1, reported, "and no other operation is reported")
}

// assertIdempotencyInference pins the idempotency policy on each operation of
// its case: a POST's Idempotency-Key header and a PUT's, through a $ref and in
// lower case, are their tokens, marked; a GET, safe by its method, a PATCH
// whose key is a query parameter, and a DELETE carrying the header twice in
// two letter cases keep the idempotency the document leaves unknown, the
// DELETE alone reported, as the one whose reading met its trigger and stopped.
func assertIdempotencyInference(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Equal(t, []jsontext.Pointer{"/paths/~1payments~1{id}/delete"},
		openapitest.PointersOf(diags, "openapi/undecided-idempotency"))
	assert.Equal(t, "no idempotency token is inferred: 2 header parameters are named Idempotency-Key, compared without case",
		openapitest.DiagMessageAt(t, diags, "openapi/undecided-idempotency", ir.SeverityInfo,
			"/paths/~1payments~1{id}/delete"))
	for _, tc := range []struct {
		operation string
		token     ir.ParameterID
	}{
		{"createPayment", "parameter/openapi/paths/~1payments/post/parameters/Idempotency-Key/header"},
		{"replacePayment", "parameter/openapi/paths/~1payments~1{id}/put/parameters/idempotency-key/header"},
		{"listPayments", ""},
		{"patchPayment", ""},
		{"deletePayment", ""},
	} {
		operation, ok := operationByName(doc, tc.operation)
		require.True(t, ok, tc.operation)
		if tc.token == "" {
			assert.Equal(t, ir.Idempotency{}, operation.Idempotency, tc.operation)
			assert.Empty(t, operation.Provenance.Inferred, tc.operation)
			continue
		}
		assert.Equal(t, ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: tc.token}, operation.Idempotency, tc.operation)
		assert.Equal(t, openapi.IdempotencyInferenceHeuristic, operation.Provenance.Inferred, tc.operation)
	}
}
