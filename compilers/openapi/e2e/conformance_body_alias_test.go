// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// assertBodyAlias is the matrix's body-alias row: a body whose schema is a
// reference to a reference to a model reads as that model's members, wherever
// a step reads one. A link and the inference of a page walk read the alias as
// one hop away, and a pagination an extension states reads it two hops away;
// no step reports a member it cannot find, since the model holds each.
func assertBodyAlias(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	assert.Empty(t, diags, "a body read through an alias draws no finding")
	nextCursor := &ir.PropPath{Segments: []ir.PropID{componentProp("Page/properties/next_cursor")}}

	pets := linkOp(t, doc, "listPets")
	require.Len(t, pets.Responses, 1)
	require.Len(t, pets.Responses[0].Links, 1, "the link is lowered, not kept verbatim")
	link := pets.Responses[0].Links[0]
	assert.Equal(t, "next", link.Name.Source)
	assert.Equal(t, pets.ID, link.Operation)
	require.Len(t, link.Params, 1)
	assert.Equal(t, ir.LinkValue{Kind: ir.LinkResponse, Response: nextCursor}, link.Params[0].Value,
		"the cursor reads the member of the model the alias stands for")
	for key := range pets.Responses[0].Unmodeled {
		assert.False(t, strings.HasPrefix(key, "openapi:links"), "the link is kept nowhere verbatim: %s", key)
	}
	for key := range pets.Unmodeled {
		assert.False(t, strings.HasPrefix(key, "openapi:links"), "the link is kept nowhere verbatim: %s", key)
	}
	require.NotNil(t, pets.Pagination, "the walk the link names is a page walk")
	assert.Equal(t, ir.PageStrategyCursor, pets.Pagination.Strategy)
	assert.True(t, pets.Pagination.Inferred)
	assert.Equal(t, nextCursor, pets.Pagination.NextCursor)

	dogs := linkOp(t, doc, "listDogs")
	cursor := ir.ParamID("param/openapi/paths/~1dogs/get/parameters/cursor/query")
	assert.Equal(t, &ir.Pagination{
		Strategy:    ir.PageStrategyCursor,
		Inferred:    true,
		InputCursor: &ir.InputPath{Param: &cursor},
		Items:       &ir.PropPath{Segments: []ir.PropID{componentProp("Page/properties/result")}},
		NextCursor:  nextCursor,
	}, dogs.Pagination, "the stated pagination reads two aliases of the model")
	assert.Equal(t, ir.Inferred("extension-promotion"), dogs.Provenance.Inferred)
}

// TestBodyAlias_ReadsTheSameInEitherOrder compiles body-alias with every
// mapping's entries reversed, the aliases declared after the model they stand
// for and the operation that reads one after the operation that reads another,
// and holds it to the same facts.
func TestBodyAlias_ReadsTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(conformanceDir, "body-alias.yaml"))
	require.NoError(t, err)
	doc, diags := parseFull(t, string(reversed(t, string(src))))
	assertBodyAlias(t, doc, diags)
}

// TestBodyAlias_ReadsAnOperationInANestedGroup compiles body-alias at 3.2 with
// both operations tagged under a tag whose parent is another, so they sit in
// a nested group, and holds it to the same facts: every step reading the
// finished document reaches an operation however deep its group.
func TestBodyAlias_ReadsAnOperationInANestedGroup(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(conformanceDir, "body-alias.yaml"))
	require.NoError(t, err)
	spec := strings.Replace(string(src), "openapi: 3.1.0\n", "openapi: 3.2.0\n"+
		"tags: [{name: animals}, {name: pets, parent: animals}]\n", 1)
	spec = strings.ReplaceAll(spec, "    get:\n", "    get:\n      tags: [pets]\n")
	require.Equal(t, 2, strings.Count(spec, "tags: [pets]"), "both operations are tagged")
	doc, diags := parseFull(t, spec)

	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	require.Len(t, doc.Services[0].Groups[0].Groups, 1, "the pets group nests under animals")
	assert.Empty(t, doc.Services[0].Groups[0].Operations, "no operation sits in the top group")
	assert.Len(t, doc.Services[0].Groups[0].Groups[0].Operations, 2, "both sit in the nested one")
	assertBodyAlias(t, doc, diags)
}
