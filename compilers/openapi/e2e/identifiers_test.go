// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test // external test package — exercises only the public API

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// splitPet is a document of schemas that declares a resource by $id, with an
// anchor and a definition inside it.
const splitPet = `Pet:
  $id: 'https://x.test/pet'
  $defs:
    Tag: {$anchor: tag, type: string}
  properties:
    a: {$ref: '#/$defs/Tag'}
    b: {$ref: '#tag'}
`

// specNaming returns a source with the component schemas given, written in order.
func specNaming(schemas ...string) string {
	return "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
		strings.Join(schemas, "")
}

// TestCompile_WhatAnotherDocumentDeclaresAnswersAReference pins that a pointer
// below an $id another document declares, a plain name inside it, and the URI
// itself all land on what that document declares, in either declaration order,
// and that a plain name read against the document that holds the resource is
// not that resource's, as in the source.
func TestCompile_WhatAnotherDocumentDeclaresAnswersAReference(t *testing.T) {
	t.Parallel()
	pet := "    Pet: {$ref: 'other.yaml#/Pet'}\n"
	byID := "    ById: {$ref: 'https://x.test/pet'}\n"
	for _, schemas := range [][]string{{pet, byID}, {byID, pet}} {
		dir := t.TempDir()
		layout(t, dir, map[string]string{"other.yaml": splitPet})
		doc, diags := compileOnDisk(t, filepath.Join(dir, "spec.yaml"), specNaming(schemas...), false)
		assertNoErrorDiags(t, diags)

		other := ids.In("other.yaml")
		byIDAlias, ok := typeByName(doc, "ById").(*ir.Scalar)
		require.True(t, ok)
		assert.Equal(t, other.ForPointer("/Pet"), byIDAlias.Base.Target)
		model, ok := doc.Types[other.ForPointer("/Pet")].(*ir.Model)
		require.True(t, ok)
		for _, wire := range []string{"a", "b"} {
			p, found := propByWire(model, wire)
			require.True(t, found)
			assert.Equal(t, other.ForPointer("/Pet/$defs/Tag"), p.Type.Target, wire)
		}
		assert.Len(t, doc.Sources, 2)
	}

	dir := t.TempDir()
	layout(t, dir, map[string]string{"other.yaml": splitPet})
	_, diags := compileOnDisk(t, filepath.Join(dir, "spec.yaml"), specNaming("    Tag: {$ref: 'other.yaml#tag'}\n"), false)
	bad := unresolvedRefs(diags)
	require.Len(t, bad, 1)
	assert.Contains(t, bad[0].Message, "no schema of the resource it names declares that name")
}

// TestCompile_AnIdentifierIsNotRequested pins that a URI a document read during
// the pass declares reaches no network with external references allowed, in
// either declaration order: its read waits for the pass to end.
func TestCompile_AnIdentifierIsNotRequested(t *testing.T) {
	t.Parallel()
	pet := "    Pet: {$ref: 'other.yaml#/Pet'}\n"
	byID := "    ById: {$ref: 'https://x.test/pet'}\n"
	for _, schemas := range [][]string{{pet, byID}, {byID, pet}} {
		dir := t.TempDir()
		layout(t, dir, map[string]string{"other.yaml": splitPet})
		recorded := &recordingReader{inner: reader.Disk{}}
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "spec.yaml"), Data: []byte(specNaming(schemas...))}},
			compilers.Options{Reader: recorded, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		require.NotNil(t, doc)
		assertNoErrorDiags(t, diags)
		for _, location := range recorded.locations() {
			assert.NotContains(t, location, "x.test")
		}
	}
}

// TestCompile_ADocumentNothingReachedIsNoSource pins the document a pass read as
// a location, before another declared that URI. It lowers nothing, so it is no
// input of the result, in either order.
func TestCompile_ADocumentNothingReachedIsNoSource(t *testing.T) {
	t.Parallel()
	a := "    A: {$ref: 'a.yaml#/$defs/T'}\n"
	z := "    Z: {$ref: 'z.yaml#/S'}\n"
	for _, schemas := range [][]string{{a, z}, {z, a}} {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		layout(t, dir, map[string]string{
			"a.yaml": "$defs: {T: {type: integer}}\n",
			"z.yaml": "S: {$id: '" + filepath.Join(dir, "a.yaml") + "', $defs: {T: {type: string}}}\n",
		})
		doc, diags := compileOnDisk(t, filepath.Join(dir, "spec.yaml"), specNaming(schemas...), true)
		assertNoErrorDiags(t, diags)
		var read []string
		for _, s := range doc.Sources {
			read = append(read, filepath.Base(s.Path))
		}
		assert.Equal(t, []string{"spec.yaml", "z.yaml"}, read)
	}
}

// TestCompile_ADynamicRefExpandsInTheDocumentItIsWrittenIn pins the reference
// another document writes: it expands to the type that document's component
// lowers to, in that document's namespace, whichever component lowers first.
func TestCompile_ADynamicRefExpandsInTheDocumentItIsWrittenIn(t *testing.T) {
	t.Parallel()
	const other = `openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  schemas:
    Tree:
      $dynamicAnchor: node
      type: object
      properties:
        children: {type: array, items: {$dynamicRef: "#node"}}
    Leaf: {$dynamicRef: "#node"}
`
	tree := "    Tree: {$ref: './other.yaml#/components/schemas/Tree'}\n"
	leaf := "    Leaf: {$ref: './other.yaml#/components/schemas/Leaf'}\n"
	docs := make([]*ir.Document, 0, 2)
	for _, schemas := range [][]string{{tree, leaf}, {leaf, tree}} {
		dir := t.TempDir()
		layout(t, dir, map[string]string{"other.yaml": other})
		doc, diags := compileOnDisk(t, filepath.Join(dir, "spec.yaml"), specNaming(schemas...), false)
		assertNoErrorDiags(t, diags)
		docs = append(docs, doc)

		in := ids.In("other.yaml")
		target := in.ForPointer("/components/schemas/Tree")
		items := in.ForPointer("/components/schemas/Tree/properties/children/items")
		alias, ok := doc.Types[items].(*ir.Scalar)
		require.True(t, ok, "%+v", diags)
		assert.Equal(t, target, alias.Base.Target)
		assert.Empty(t, alias.Unmodeled, "an expanded reference is not kept as well")
		assert.Equal(t, 1, countCode(diags, "openapi/dynamic-ref-expanded", "/components/schemas/Tree/properties/children/items/$dynamicRef"))
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types), "the declaration order decides nothing")
}

// TestCompile_ADynamicRefNoDocumentPlacesIsKept pins content a reference object
// resolved to that no position of its document places: nothing says whose anchor
// it names, so it is kept, and never read against the source's.
func TestCompile_ADynamicRefNoDocumentPlacesIsKept(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{"ext.yaml": `q:
  name: q
  in: query
  schema: {type: array, items: {$dynamicRef: '#node'}}
q: {name: r, in: query, schema: {type: string}}
`})
	src := strings.Replace(specNaming("    Node: {$dynamicAnchor: node, type: object}\n"), "paths: {}",
		"paths:\n  /a: {get: {parameters: [{$ref: 'ext.yaml#/q'}], responses: {'200': {description: ok}}}}", 1)
	_, diags := compileOnDisk(t, filepath.Join(dir, "spec.yaml"), src, false)
	found := false
	for _, d := range diags {
		found = found || strings.Contains(d.Message, "no position of its document places")
		assert.NotEqual(t, "openapi/dynamic-ref-expanded", d.Code)
	}
	assert.True(t, found, "%+v", diags)
}

// TestCompile_ASchemaAnExpansionReachesIsValidated pins that an expansion reaches
// what it names as a $ref does: a schema of another document only a $dynamicRef
// reaches draws what the same schema draws reached by a $ref, not nothing.
func TestCompile_ASchemaAnExpansionReachesIsValidated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{"o.yaml": "E: {$dynamicRef: '#n'}\nT: {$dynamicAnchor: n, maxLength: -1}\n"})
	findings := func(ref string) []string {
		_, diags := compileOnDisk(t, filepath.Join(dir, "spec.yaml"), specNaming("    E: {$ref: '"+ref+"'}\n"), false)
		var got []string
		for _, d := range diags {
			if d.Severity == ir.SeverityError {
				got = append(got, d.Code+" "+string(d.Provenance.Pointer))
			}
		}
		return got
	}
	byRef := findings("o.yaml#/T")
	require.NotEmpty(t, byRef)
	assert.Equal(t, byRef, findings("o.yaml#/E"))
}
