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

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// TestTitleNames_DisabledNamesByPosition pins the title-names policy off
// (docs/compilers/openapi.md#naming): the title-hints case names every node
// and union variant as that document does with each schema's title written as
// its description, which names nothing, and keeps each title as the summary
// it is with the policy on, where some of those names differ.
func TestTitleNames_DisabledNamesByPosition(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(conformanceDir, "title-hints.yaml"))
	require.NoError(t, err)
	spec := string(src)
	paths := strings.Index(spec, "\npaths:")
	require.Positive(t, paths, "the info object's title stays")
	untitled := spec[:paths] + strings.ReplaceAll(spec[paths:], "title: ", "description: ")
	require.Equal(t, 1, strings.Count(untitled, "title: "), "only the info object's title is left")

	off := compileClean(t, spec, openapi.Options{TitleNames: openapi.TitleNames{Disabled: true}})
	written := compileClean(t, untitled, openapi.Options{})
	on := compileClean(t, spec, openapi.Options{})
	require.Len(t, off.Types, len(written.Types))
	differs := 0
	for id, td := range off.Types {
		require.Contains(t, written.Types, id)
		assert.Equal(t, typeNames(written.Types[id]), typeNames(td), "%s is named by its position", id)
		assert.Equal(t, on.Types[id].Common().Docs, td.Common().Docs, "%s keeps its title as its summary", id)
		if !assert.ObjectsAreEqual(typeNames(on.Types[id]), typeNames(td)) {
			differs++
		}
	}
	assert.Positive(t, differs, "the case names some node by its title with the policy on")
}

// typeNames returns the names td carries: its own, then each of its
// variants' where it is a union.
func typeNames(td ir.TypeDef) []ir.Naming {
	names := []ir.Naming{td.Common().Name}
	if u, ok := td.(*ir.Union); ok {
		for _, v := range u.Variants {
			names = append(names, v.Name)
		}
	}
	return names
}
