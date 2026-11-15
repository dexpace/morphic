// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"testing"

	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
)

// docOf reads ov as the compile reads an overlay document, into the tree Apply
// is handed.
func docOf(t testing.TB, ov string) *yaml.Node {
	t.Helper()
	doc, err := doctree.Read(t.Context(), []byte(ov), budget.Unbounded())
	require.NoError(t, err)
	return doc.Root
}
