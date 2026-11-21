// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestDiscriminator_KeptWholeOnce holds a discriminator kept verbatim on a
// type to that one entry: an extension written inside it is within the entry
// and not kept again under its own key. A property whose xml lowers to a typed
// field still keeps its extension, since nothing there holds xml whole.
func TestDiscriminator_KeptWholeOnce(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "openapi", "discriminator_kept_whole.yaml"))
	require.NoError(t, err)
	doc, _ := parseFull(t, string(src))

	node := doc.Types[ir.TypeID("type/openapi/components/schemas/S")]
	require.NotNil(t, node)
	kept := node.Common().Unmodeled
	assert.Contains(t, kept, "openapi:discriminator")
	assert.NotContains(t, kept, "openapi:discriminator/x-tag", "the extension is inside the whole entry")

	holder, ok := doc.Types[ir.TypeID("type/openapi/components/schemas/Holder")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, holder.Properties, 1)
	assert.Contains(t, holder.Properties[0].Unmodeled, "openapi:xml/x-tag")
}
