package naming

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
)

// loaded returns the table a load of spec binds.
func loaded(t *testing.T, spec string) *bind.Table {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.yaml"), []byte(spec), 0o600))
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(spec)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc.Bindings.Table()
}

// TestNode_ReadsNothingTheTableDoesNotHold pins that node reads only a
// document the table holds, and nothing a namer with no table is asked for.
func TestNode_ReadsNothingTheTableDoesNotHold(t *testing.T) {
	t.Parallel()
	tb := loaded(t, openapitest.ComponentSpec("    A: {type: object}\n"))
	n := New(tb)
	require.NotNil(t, n.node(bind.Source, "/components/schemas/A"), "sanity: the source holds A")
	assert.Nil(t, n.node(-1, "/components/schemas/A"))
	assert.Nil(t, n.node(bind.DocID(tb.Documents()), "/components/schemas/A"))
	assert.Nil(t, New(nil).node(bind.Source, "/components/schemas/A"))
}

// TestNode_ReadsNothingPastTheMergeBound pins that a position reached through
// a `<<` chain deeper than the view reads is no node: the parser may read one
// there, so reading none names nothing rather than guessing.
func TestNode_ReadsNothingPastTheMergeBound(t *testing.T) {
	t.Parallel()
	spec := func(levels int) string {
		return openapitest.ComponentSpec("    A: {type: object}\n") +
			"x-deep: " + openapitest.NestedMerges(levels, "{b: {name: x}}") + "\n"
	}
	assert.NotNil(t, New(loaded(t, spec(nodeview.MergeDepthLimit))).node(bind.Source, "/x-deep/b"))
	assert.Nil(t, New(loaded(t, spec(nodeview.MergeDepthLimit+1))).node(bind.Source, "/x-deep/b"))
}
