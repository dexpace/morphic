// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
)

// decodeStream reads data as the compile reads a source, with no node budget:
// the tree of its first document holding content, what the stream holds past
// it, and the reader's error.
func decodeStream(ctx context.Context, data []byte) (*yaml.Node, doctree.Tail, error) {
	doc, err := doctree.Read(ctx, data, budget.Unbounded())
	return doc.Root, doc.Rest, err
}

// parseTree reads data as the compile reads a document a reference names, with
// no node budget, into its tree.
func parseTree(ctx context.Context, data []byte) (*yaml.Node, error) {
	doc, err := doctree.Read(ctx, data, budget.Unbounded())
	return doc.Root, err
}

// overlayTree reads the overlay opts carries as patch reads it, with no node
// budget, into the tree applyOverlay is handed.
func overlayTree(t testing.TB, opts Options) *yaml.Node {
	t.Helper()
	doc, err := doctree.Read(t.Context(), opts.Overlay.Data, budget.Unbounded())
	require.NoError(t, err)
	return doc.Root
}

// holdsContent reports whether doc is a document node wrapping one root that is
// not the null an empty document decodes to, as the reader judges a document
// of a stream.
func holdsContent(doc *yaml.Node) bool {
	return doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 &&
		(doc.Content[0].Kind != yaml.ScalarNode || doc.Content[0].ShortTag() != "!!null")
}
