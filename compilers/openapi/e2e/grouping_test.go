// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// TestGroupingByExtension_ReportsInTheDocumentDeclaringTheOperation pins where
// a grouping extension naming no path is reported when another document
// declares its operation: there, at the key, and not in the source, whose
// method beside the $ref puts the source's layer first in the mount.
func TestGroupingByExtension_ReportsInTheDocumentDeclaringTheOperation(t *testing.T) {
	t.Parallel()
	sources := []compilers.Source{
		{Path: "root.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /items:
    $ref: './other.yaml#/components/pathItems/Item'
    post: {operationId: besideRef, x-fern-sdk-group-name: items, responses: {"200": {description: ok}}}
`)},
		{Path: "other.yaml", Data: []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  pathItems:
    Item:
      get: {operationId: listItems, x-fern-sdk-group-name: [items, 7], responses: {"200": {description: ok}}}
`)},
	}
	docs, err := reader.NewMemory(sources)
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(), sources[:1],
		compilers.Options{Reader: docs, FormatOptions: openapi.Options{Grouping: openapi.GroupByExtension}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	require.Len(t, diags, 1, "%v", diags)
	assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
	assert.Equal(t, 1, diags[0].Provenance.Source, "the document declaring the operation, not the source")
	assert.Equal(t, jsontext.Pointer("/components/pathItems/Item/get/x-fern-sdk-group-name"), diags[0].Provenance.Pointer)
	require.Len(t, doc.Sources, 2)
	assert.Equal(t, "other.yaml", doc.Sources[diags[0].Provenance.Source].Path)
}
