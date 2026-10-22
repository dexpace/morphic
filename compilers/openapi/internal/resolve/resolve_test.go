package resolve_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
)

// TestScope_InDocumentOf_ReadsWhereTheBindingsPlaceTheSchema pins the scope a
// schema's content is read in: the source's own for one the source holds,
// whatever scope it is asked from, and the other document's for one the
// bindings place there, named as they read it. A schema the bindings do not
// know, and any asked of a scope without bindings, is read as the source's.
func TestScope_InDocumentOf_ReadsWhereTheBindingsPlaceTheSchema(t *testing.T) {
	t.Parallel()
	path := externalDocument(t)
	b := loadBound(t, path, openapitest.ComponentSpec(targetsSpec), load.Options{Reader: reader.Disk{}})
	foreign := b.scope
	foreign.Foreign, foreign.Holder = true, "elsewhere.yaml"

	home := foreign.InDocumentOf(b.component(t, "Target"))
	assert.False(t, home.Foreign, "the source holds Target")
	assert.Empty(t, home.Holder)

	e := b.scope.Bindings.Declared(b.component(t, "Other"))
	require.NotNil(t, e, "Other's $ref lands on E")
	there := b.scope.InDocumentOf(e)
	assert.True(t, there.Foreign, "ext.yaml holds E")
	assert.Equal(t, filepath.Join(filepath.Dir(path), "ext.yaml"), there.Holder)

	assert.False(t, foreign.InDocumentOf(&schemaRef{}).Foreign, "a schema the bindings do not know")
	unbound := foreign
	unbound.Bindings = nil
	assert.False(t, unbound.InDocumentOf(e).Foreign, "a scope without bindings")
}
