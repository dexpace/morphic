// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// pageTruePathItem is a path item stating a cursor walk, a GET writing the
// pagination key true, and a DELETE writing nothing.
const pageTruePathItem = "../../../../testdata/openapi/page_true_beside_path_item.yaml"

// TestPromote_TrueReadsThePathItemFirst holds a pagination key written true on
// an operation to the walk its path item states, which is nearer than the
// root's: the operation is filled as if it had written that object, marked as
// promoted, and no finding says the key states nothing.
func TestPromote_TrueReadsThePathItemFirst(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(pageTruePathItem)
	require.NoError(t, err)
	doc, diags := compile(t, string(src), openapi.Options{})

	list := openapitest.FindOperation(t, doc, "listDogs")
	require.NotNil(t, list.Pagination, "the path item's statement fills the operation that wrote true")
	got, err := json.Marshal(list.Pagination, json.Deterministic(true))
	require.NoError(t, err)
	assert.JSONEq(t, `{"strategy":"cursor","inferred":true,`+
		`"inputCursor":{"param":"parameter/openapi/paths/~1dogs/get/parameters/cursor/query"},`+
		`"items":{"segments":["property/openapi/components/schemas/Page/properties/rows"]},`+
		`"nextCursor":{"segments":["property/openapi/components/schemas/Page/properties/nxt"]}}`, string(got))
	assert.Equal(t, "extension-promotion", string(list.Provenance.Inferred))
	assert.Empty(t, openapitest.PointersOf(diags, diag.UnpromotedExtension),
		"a statement read from the path item draws no finding")
}

// TestPromote_ASilentOperationReadsNeitherItsPathItemNorTheRoot holds an
// operation that wrote no pagination key to none: a path item's statement of a
// walk says nothing of a method that did not opt in. The path item's key stays
// kept on the operation.
func TestPromote_ASilentOperationReadsNeitherItsPathItemNorTheRoot(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(pageTruePathItem)
	require.NoError(t, err)
	doc, diags := compile(t, string(src), openapi.Options{})

	purge := openapitest.FindOperation(t, doc, "purgeDogs")
	assert.Nil(t, purge.Pagination)
	assert.NotContains(t, string(purge.Provenance.Inferred), "extension-promotion")
	assert.Contains(t, purge.Unmodeled, "openapi:pathItem/x-fern-pagination")
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.UnpromotedExtension {
			assert.NotContains(t, string(diagnostic.Provenance.Pointer), "/delete", "no finding names purgeDogs")
		}
	}
}
