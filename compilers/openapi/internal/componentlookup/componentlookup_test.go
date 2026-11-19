// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package componentlookup_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/engine/reader"
)

// TestScope_InDocument_ReadsTheDocumentItNames pins the scope content is read
// in by the document holding it: the source's own, whatever scope it is asked
// from, and another document's as the bindings read it, named by its path.
// The scope it is asked from is left as it was.
func TestScope_InDocument_ReadsTheDocumentItNames(t *testing.T) {
	t.Parallel()
	path := externalDocument(t)
	b := loadBound(t, path, openapitest.ComponentSpec("    Other: {$ref: 'ext.yaml#/components/schemas/E'}\n"),
		load.Options{Reader: reader.Disk{}})
	foreign := b.scope
	foreign.Foreign, foreign.Holder = true, "elsewhere.yaml"

	home := foreign.InDocument(reftable.Source)
	assert.False(t, home.Foreign, "the source's own content")
	assert.Empty(t, home.Holder)
	assert.True(t, foreign.Foreign, "the scope it was asked from is left as it was")

	e, ok := b.doc.Bindings.Entry(b.component(t, "Other"))
	require.True(t, ok && e.Lands(), "Other's $ref lands in ext.yaml")
	there := b.scope.InDocument(e.Hop.Target.Doc)
	assert.True(t, there.Foreign, "ext.yaml holds E")
	assert.Equal(t, filepath.Join(filepath.Dir(path), "ext.yaml"), there.Holder)
	assert.False(t, b.scope.Foreign, "the scope it was asked from is left as it was")
}
