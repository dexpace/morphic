// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it snapshot-
// compares the whole compiler's IR output against a byte-exact golden file, so
// it has no single source file to pair with.
package e2e_test // external test package — exercises only the public API

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestGolden lowers a full petstore-style document and compares its IR against a
// byte-exact golden snapshot. Regenerate it with
// `go test ./compilers/openapi/e2e -run TestGolden -update`.
func TestGolden(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../testdata/golden/openapi/petstore.yaml")
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "petstore.yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	irtest.CompareGolden(t, "../../../testdata/golden/openapi/petstore.golden.json", doc)
}

// TestGolden_HintPositions pins the IR of the spec written to take each decision
// a hint is composed by within one valid document. ir's
// TestIRVersion_PinsTheNames lists the hints this golden holds, so a hint moved
// here moves IRVersion (docs/ir/ir-version.md).
func TestGolden_HintPositions(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../testdata/golden/openapi/hint_positions.yaml")
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "hint_positions.yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	irtest.CompareGolden(t, "../../../testdata/golden/openapi/hint_positions.golden.json", doc)
}

// TestGolden_SplitSpec pins the IR of a spec split across four files, read
// from memory under the paths they are named by: each other document is a
// Document.Sources entry after the source, ordered by its path from the
// source's directory, and what it declares is lowered where it declares it,
// in namespaces qualified by that path. paths.yaml's path item writes
// references of its own, into itself, a third file and the source, and
// common.yaml's response one into itself (GitHub #751). It and its operation
// write servers too, registered in paths.yaml's namespace at their own
// positions there, which the operations they serve name.
func TestGolden_SplitSpec(t *testing.T) {
	t.Parallel()
	sources := make([]compilers.Source, 0, 4)
	for _, name := range []string{"root.yaml", "common.yaml", "paths.yaml", "schemas/pet.yaml"} {
		data, err := os.ReadFile("../../../testdata/golden/openapi/split/" + name)
		require.NoError(t, err)
		sources = append(sources, compilers.Source{Path: "split/" + name, Data: data})
	}
	docs, err := reader.NewMemory(sources)
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(), sources[:1], compilers.Options{Reader: docs})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)

	const item = "server/openapi@paths.yaml/paths/~1pets~1{petId}"
	byURL := map[string]ir.ServerID{}
	for id, s := range doc.Servers {
		byURL[s.URLTemplate] = id
		assert.Equal(t, string(id), "server/"+serverSpace(t, id)+string(s.Provenance.Pointer),
			"%s is named by the position its provenance records", id)
	}
	assert.Equal(t, ir.ServerID(item+"/servers/0"), byURL["https://pets.example.com"])
	assert.Equal(t, ir.ServerID(item+"/get/servers/0"), byURL["https://show.pets.example.com"])
	ops := allOperations(doc)
	for _, op := range ops {
		if op.Name.Source == "showPet" {
			assert.Equal(t, []ir.ServerID{item + "/get/servers/0"}, op.Servers, "showPet is sent to its own")
		}
	}
	// Last, and only if they held: under -update a failed assertion would
	// otherwise leave a golden that agrees with the regression.
	require.False(t, t.Failed(), "an assertion failed; not comparing or rewriting the golden")
	irtest.CompareGolden(t, "../../../testdata/golden/openapi/split/split.golden.json", doc)
}

// serverSpace returns the namespace segment of a server's ID.
func serverSpace(t *testing.T, id ir.ServerID) string {
	t.Helper()
	space, ok := ir.IDSpace(ir.IDKindServer, string(id))
	require.True(t, ok, "%s is a well-formed server ID", id)
	return space
}
