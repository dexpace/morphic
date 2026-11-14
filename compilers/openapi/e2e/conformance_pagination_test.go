// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// pagedBody is the ID of the member a JSON response of the operation method
// at path (a path with its slashes written ~1) declares at the member path.
func pagedBody(path, method, member string) ir.PropID {
	return ir.PropID("p/openapi/paths/~1" + path + "/" + method +
		"/responses/200/content/application~1json/schema/properties/" + member)
}

// assertPaginationFlag is the matrix's pagination-flag row: a cursor walk
// whose page states a flag beside its next cursor ends at the flag and lists
// no other stop, and is marked as inferred.
func assertPaginationFlag(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, diags, "a flag beside a cursor draws no finding")
	op := linkOp(t, doc, "listChanges")
	cursor := linkParam(t, op, "cursor")
	info := pagedBody("changes", "get", "result_info")
	assert.Equal(t, &ir.Pagination{
		Strategy:    ir.PageStrategyCursor,
		Inferred:    true,
		InputCursor: &ir.InputPath{Param: &cursor},
		Items:       &ir.PropPath{Segments: []ir.PropID{pagedBody("changes", "get", "result")}},
		NextCursor:  &ir.PropPath{Segments: []ir.PropID{info, info + "/properties/next_cursor"}},
		HasMore:     &ir.PropPath{Segments: []ir.PropID{info, info + "/properties/has_more"}},
		Stop:        []ir.PageStop{ir.PageStopNoMore},
	}, op.Pagination)
	assert.Equal(t, openapi.PaginationInferenceHeuristic, op.Provenance.Inferred)
}

// assertPaginationBody is the matrix's pagination-body row: a POST whose
// request body holds the page and the page size, and whose response holds the
// items at its root, is a page walk whose inputs address the body.
func assertPaginationBody(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, diags, "a search POST read as a walk draws no finding")
	op := linkOp(t, doc, "search")
	body := ir.PropID("p/openapi/paths/~1search/post/requestBody/content/application~1json/schema/properties/")
	assert.Equal(t, &ir.Pagination{
		Strategy:    ir.PageStrategyPage,
		Inferred:    true,
		InputCursor: &ir.InputPath{Segments: []ir.PropID{body + "page"}},
		InputLimit:  &ir.InputPath{Segments: []ir.PropID{body + "per_page"}},
		Items:       &ir.PropPath{Segments: []ir.PropID{pagedBody("search", "post", "result")}},
	}, op.Pagination)
	assert.Nil(t, op.Pagination.InputCursor.Param, "a body member is addressed from the body, not from a parameter")
	assert.Equal(t, openapi.PaginationInferenceHeuristic, op.Provenance.Inferred)
}

// assertPaginationLinks is the matrix's pagination-links row: the links a page
// states, typed as URLs, are read beside the walk the page number names.
func assertPaginationLinks(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, diags, "links beside a page number draw no finding")
	op := linkOp(t, doc, "listThings")
	page := linkParam(t, op, "page")
	info := pagedBody("things", "get", "result_info")
	assert.Equal(t, &ir.Pagination{
		Strategy:    ir.PageStrategyPage,
		Inferred:    true,
		InputCursor: &ir.InputPath{Param: &page},
		Items:       &ir.PropPath{Segments: []ir.PropID{pagedBody("things", "get", "result")}},
		NextLink:    &ir.PropPath{Segments: []ir.PropID{info, info + "/properties/next"}},
		PrevLink:    &ir.PropPath{Segments: []ir.PropID{info, info + "/properties/previous"}},
	}, op.Pagination)
	assert.Equal(t, openapi.PaginationInferenceHeuristic, op.Provenance.Inferred)
}

// assertExtensionPaginationRoot is the matrix's extension-pagination-root
// row: an operation writing the pagination key true takes the walk the root
// states, marked as promoted, and the root's entry and the operation's own
// stay kept.
func assertExtensionPaginationRoot(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, diags, "a true that reads the root's walk draws no finding")
	op := linkOp(t, doc, "listThings")
	token := linkParam(t, op, "page_token")
	assert.Equal(t, &ir.Pagination{
		Strategy:    ir.PageStrategyCursor,
		Inferred:    true,
		InputCursor: &ir.InputPath{Param: &token},
		Items:       &ir.PropPath{Segments: []ir.PropID{pagedBody("things", "get", "data")}},
		NextCursor:  &ir.PropPath{Segments: []ir.PropID{pagedBody("things", "get", "next_page_token")}},
	}, op.Pagination)
	assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
	assert.Contains(t, doc.Unmodeled, "openapi:x-fern-pagination", "the root's entry stays kept")
	assert.Contains(t, op.Unmodeled, "openapi:x-fern-pagination", "the operation's key stays kept")
}

// TestPaginationConformance_ReadsTheSameInEitherOrder compiles each of the
// pagination specs with every mapping's entries reversed, the root's key after
// the paths it is read for and each member after the ones it is read beside,
// and holds it to the same facts.
func TestPaginationConformance_ReadsTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	for name, assertCase := range map[string]func(*testing.T, *ir.Document, []ir.Diagnostic){
		"pagination-flag":           assertPaginationFlag,
		"pagination-body":           assertPaginationBody,
		"pagination-links":          assertPaginationLinks,
		"extension-pagination-root": assertExtensionPaginationRoot,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
			require.NoError(t, err)
			doc, diags := parseFull(t, string(reversed(t, string(src))))
			assertCase(t, doc, diags)
		})
	}
}
