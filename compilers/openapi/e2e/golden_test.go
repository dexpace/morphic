// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it snapshot-
// compares the whole compiler's IR output against a byte-exact golden file, so
// it has no single source file to pair with.
package e2e_test // external test package — exercises only the public API

import (
	"encoding/json/jsontext"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
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

// TestGolden_HintPositions pins the IR of the spec written to hold one position
// for each way a hint is composed from one within a document. ir's
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

// TestGolden_ExtensionGroups pins the IR of a spec grouped by the path its
// default grouping extension states, each spelling of the path beside an
// operation stating none and one stating a value that names no path, both of
// which fall back to their tags. That one is reported, as info, and nothing
// else is.
func TestGolden_ExtensionGroups(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../testdata/golden/openapi/extension_groups.yaml")
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "extension_groups.yaml", Data: data}},
		compilers.Options{FormatOptions: openapi.Options{Grouping: openapi.GroupByExtension}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, diags, 1, "%v", diags)
	assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
	assert.Equal(t, jsontext.Pointer("/paths/~1legacy/get/x-fern-sdk-group-name"), diags[0].Provenance.Pointer)
	irtest.CompareGolden(t, "../../../testdata/golden/openapi/extension_groups.golden.json", doc)
}

// TestGolden_AllOfLentDescription pins GitHub #2152 in the order a fold by
// declaration order alone got wrong: the allOf entry describing a member comes
// before the entry whose declaration of it is a bare $ref to a described
// target. The member keeps the description written, nothing is reported, and
// the target keeps its own.
func TestGolden_AllOfLentDescription(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../testdata/golden/openapi/allof_lent_description.yaml")
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "allof_lent_description.yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, diags)

	items, ok := doc.Types["type/anonymous/components/schemas/Response/properties/Warnings/items"].(*ir.Model)
	require.True(t, ok)
	require.Len(t, items.Properties, 1)
	assert.Equal(t, "The price before the change.", items.Properties[0].Docs.Description)
	assert.Empty(t, items.Properties[0].Unmodeled)
	assert.Equal(t, "Container for price information.", doc.Types["type/openapi/components/schemas/Money"].Common().Docs.Description)
	// Last: under -update the compare refuses a test whose assertions failed.
	irtest.CompareGolden(t, "../../../testdata/golden/openapi/allof_lent_description.golden.json", doc)
}

// TestGolden_EncodingNestedReferences pins the IR of references beneath the
// Encoding Objects OpenAPI 3.2 nests under another's encoding, prefixEncoding
// and itemEncoding, and of references into them. Each nested one is kept
// verbatim, a schema one references outside the components is lowered as one a
// kept keyword references is, and component schemas and a response header
// that reference a position beneath one read it where it is written, a header
// schema's items named by the header's key, whether or not a reference to the
// header reaches it too.
func TestGolden_EncodingNestedReferences(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../testdata/golden/openapi/encoding_nested_references.yaml")
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "encoding_nested_references.yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)

	const encoding = "type/anonymous/paths/~1parts/post/requestBody/content/multipart~1form-data/encoding"
	const header = encoding + "/doc/encoding/inner/headers/X-Inner/schema"
	for _, ref := range []struct{ name, items, key string }{
		{"Into", header + "/items", "X-Inner"},
		{"Only", encoding + "/parts/prefixEncoding/0/headers/X-Only/schema/items", "X-Only"},
	} {
		assert.Equal(t, ir.TypeID(ref.items), useTarget(t, doc, ref.name), ref.name)
		items, ok := doc.Types[ir.TypeID(ref.items)]
		require.True(t, ok, ref.name)
		assert.Equal(t, compile.SubHint(ref.key, "item"), items.Common().Name.Hint, ref.name)
	}
	operation := operationAt(t, doc, "POST", "/parts")
	back, ok := headerByWire(operation.Responses[0].Headers, "X-Back")
	require.True(t, ok)
	assert.Equal(t, ir.TypeID(header), back.Type.Target)
	for _, nested := range []string{"doc/encoding", "parts/prefixEncoding", "parts/itemEncoding"} {
		entry := unmodeledEntry(t, requestContent(t, operation).Unmodeled, "openapi:encoding/"+nested)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason, nested)
	}
	assert.Contains(t, doc.Types, ir.TypeID("type/anonymous/x-defs/Kept"), "a schema a nested header references")
	// Last: under -update the compare refuses a test whose assertions failed.
	irtest.CompareGolden(t, "../../../testdata/golden/openapi/encoding_nested_references.golden.json", doc)
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
	for id, server := range doc.Servers {
		byURL[server.URLTemplate] = id
		assert.Equal(t, string(id), "server/"+serverSpace(t, id)+string(server.Provenance.Pointer),
			"%s is named by the position its provenance records", id)
	}
	assert.Equal(t, ir.ServerID(item+"/servers/0"), byURL["https://pets.example.com"])
	assert.Equal(t, ir.ServerID(item+"/get/servers/0"), byURL["https://show.pets.example.com"])
	operations := allOperations(doc)
	for _, operation := range operations {
		if operation.Name.Source == "showPet" {
			assert.Equal(t, []ir.ServerID{item + "/get/servers/0"}, operation.Servers, "showPet is sent to its own")
		}
	}
	// Last: under -update the compare refuses a test whose assertions failed,
	// which would otherwise leave a golden that agrees with the regression.
	irtest.CompareGolden(t, "../../../testdata/golden/openapi/split/split.golden.json", doc)
}

// serverSpace returns the namespace segment of a server's ID.
func serverSpace(t *testing.T, id ir.ServerID) string {
	t.Helper()
	space, ok := ir.IDSpace(ir.IDKindServer, string(id))
	require.True(t, ok, "%s is a well-formed server ID", id)
	return space
}
