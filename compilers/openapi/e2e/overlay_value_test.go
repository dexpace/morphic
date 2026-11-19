// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// sharedSpec anchors schema A, which B names by an alias and C by a merge key.
const sharedSpec = srMin + `components:
  schemas:
    A: &a {type: object, properties: {x: {type: string}}}
    B: *a
    C: {<<: *a, description: c}
`

// expandedSpec is sharedSpec with every alias and merge written out by hand.
const expandedSpec = srMin + `components:
  schemas:
    A: {type: object, properties: {x: {type: string}}}
    B: {type: object, properties: {x: {type: string}}}
    C: {type: object, properties: {x: {type: string}}, description: c}
`

// describeA is an Overlay 1.1 document that describes schema A alone.
const describeA = `overlay: 1.1.0
info: {title: o, version: "1"}
actions:
  - target: $.components.schemas.A
    update: {description: only A}
`

// compileWithPatch compiles spec with the overlay applied strictly.
func compileWithPatch(t *testing.T, spec, patch string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return compileText(t, "spec.yaml", spec, openapi.Options{Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(patch)}})
}

// schemaDescription returns the description the IR gives the component schema.
func schemaDescription(t *testing.T, doc *ir.Document, name string) string {
	t.Helper()
	node := findByID(irTree(t, doc), string(componentID(name)))
	require.NotNil(t, node, "no type for %q", name)
	docs, ok := node["docs"].(map[string]any)
	if !ok {
		return ""
	}
	description, _ := docs["description"].(string)
	return description
}

// TestCompile_AnOverlayDescribesOneSchemaOfASharedNode pins the issue an
// overlay's selector once had with a YAML alias: describing the anchored schema
// describes it alone, with no warning, and the IR is the one the overlay gives
// the same source written with every alias and merge expanded.
func TestCompile_AnOverlayDescribesOneSchemaOfASharedNode(t *testing.T) {
	t.Parallel()
	shared, diags := compileWithPatch(t, sharedSpec, describeA)
	require.NotNil(t, shared, "%+v", diags)
	assert.Empty(t, diags, "no warning for what the source shares")

	assert.Equal(t, "only A", schemaDescription(t, shared, "A"))
	assert.Empty(t, schemaDescription(t, shared, "B"), "the alias keeps the value it stood for")
	assert.Equal(t, "c", schemaDescription(t, shared, "C"), "and so does the merge, with what it wrote")
	for _, name := range []string{"A", "B", "C"} {
		model, ok := typeByName(shared, name).(*ir.Model)
		require.True(t, ok, name)
		_, has := propByWire(model, "x")
		assert.True(t, has, "%s still holds x", name)
	}

	written, diags := compileWithPatch(t, expandedSpec, describeA)
	require.NotNil(t, written, "%+v", diags)
	assert.Empty(t, diffIgnoringOrigin(written, shared), "the shared source gives the IR its expansion does (-expanded +shared)")
}

// TestCompile_AnOverlayLeavesTheSourceAsItFound pins copy on write across
// compiles: the bytes handed in are as they were, compiling the source without
// the overlay afterwards gives what it gave before, and compiling it with the
// overlay twice gives one document.
func TestCompile_AnOverlayLeavesTheSourceAsItFound(t *testing.T) {
	t.Parallel()
	data := []byte(sharedSpec)
	compile := func(overlay *openapi.Overlay) *ir.Document {
		t.Helper()
		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: data}},
			compilers.Options{FormatOptions: openapi.Options{Overlay: overlay}})
		require.NoError(t, err)
		require.NotNil(t, doc, "%+v", diags)
		return doc
	}
	patch := &openapi.Overlay{Path: "patch.yaml", Data: []byte(describeA)}

	before := compile(nil)
	first := compile(patch)
	second := compile(patch)
	after := compile(nil)

	assert.Equal(t, sharedSpec, string(data), "the source's bytes")
	assert.Equal(t, describeA, string(patch.Data), "the overlay's bytes")
	assert.Equal(t, jsonOf(t, first), jsonOf(t, second), "the same overlay twice")
	assert.Equal(t, jsonOf(t, before), jsonOf(t, after), "the source after the overlay is the source before it")
	assert.Equal(t, before.Sources, after.Sources)
	assert.NotEqual(t, jsonOf(t, before), jsonOf(t, first), "and the overlay did write something")
}

// TestCompile_AJSONSourceAndItsOverlayGiveTheYAMLExpansionsIR pins the same
// patch written in JSON, which holds no alias to share: its IR is the one the
// YAML source with its aliases expanded gives.
func TestCompile_AJSONSourceAndItsOverlayGiveTheYAMLExpansionsIR(t *testing.T) {
	t.Parallel()
	const jsonSpec = `{"openapi": "3.1.0", "info": {"title": "t", "version": "1"}, "paths": {}, "components": {"schemas": {
  "A": {"type": "object", "properties": {"x": {"type": "string"}}},
  "B": {"type": "object", "properties": {"x": {"type": "string"}}},
  "C": {"type": "object", "properties": {"x": {"type": "string"}}, "description": "c"}}}}`
	const jsonPatch = `{"overlay": "1.1.0", "info": {"title": "o", "version": "1"}, "actions": [
  {"target": "$.components.schemas.A", "update": {"description": "only A"}}]}`

	fromJSON, diags := compileWithPatch(t, jsonSpec, jsonPatch)
	require.NotNil(t, fromJSON, "%+v", diags)
	assert.Empty(t, diags)
	fromYAML, diags := compileWithPatch(t, expandedSpec, describeA)
	require.NotNil(t, fromYAML, "%+v", diags)

	assert.Equal(t, "only A", schemaDescription(t, fromJSON, "A"))
	assert.Equal(t, jsonOf(t, fromYAML), jsonOf(t, fromJSON))
}

// TestCompile_AnOverlay12AppliesAndNamesItsSelf pins Overlay 1.2 through the
// compile: a reusable action referenced at a target writes what its fields
// write, there is no finding that the version is unsupported, and the
// overlay's entry in Document.Sources records the version and the $self.
func TestCompile_AnOverlay12AppliesAndNamesItsSelf(t *testing.T) {
	t.Parallel()
	const patch = `overlay: 1.2.0
info: {title: o, version: "1"}
$self: https://example.com/o.yaml
components:
  actions:
    describe:
      description: reusable
      fields:
        description: from fields
        update: {description: d}
actions:
  - $ref: '#/components/actions/describe'
    target: $.info
`
	doc, diags := compileWithPatch(t, srMin, patch)

	require.NotNil(t, doc, "%+v", diags)
	assert.Empty(t, diags)
	assert.Equal(t, "d", doc.Docs.Description)
	require.Len(t, doc.Sources, 2)
	assert.Equal(t, "overlay@1.2.0", doc.Sources[1].Format)
	assert.Equal(t, "https://example.com/o.yaml", doc.Sources[1].SelfURI)
	assert.Empty(t, doc.Sources[0].SelfURI, "the source declares none")
}

// TestCompile_AnOverlayFieldTheVersionDoesNotDefineRefusesTheCompile pins the
// consequence of reading an overlay by its version's fields: a misspelled
// update is an error at the field, in the overlay's source, with no document
// lowered, and no finding says an Overlay version is unsupported.
func TestCompile_AnOverlayFieldTheVersionDoesNotDefineRefusesTheCompile(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		patch := "overlay: " + version + "\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.info\n    udpate: {description: d}\n"

		doc, diags := compileWithPatch(t, srMin, patch)

		assert.Nil(t, doc, version)
		require.NotEmpty(t, diags, version)
		assert.Equal(t, diag.OverlayInvalid, diags[0].Code, "%s: %+v", version, diags)
		assert.Equal(t, ir.SeverityError, diags[0].Severity)
		assert.Equal(t, 1, diags[0].Provenance.Source, "the finding is about the overlay")
		assert.Equal(t, "/actions/0/udpate", string(diags[0].Provenance.Pointer), version)
		for _, diagnostic := range diags {
			assert.NotEqual(t, "openapi/overlay-unsupported", diagnostic.Code, "the code has no site left")
		}
	}
}
