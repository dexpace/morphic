// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapitest_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestSpecFiles_ListsTheSpecsUnderRoot pins the filter: YAML and JSON at any
// depth and in any case, in lexical order, but no IR snapshot and no other
// file.
func TestSpecFiles_ListsTheSpecsUnderRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"a.yaml", "b.YML", "c.json", "d.golden.json", "e.txt", "sub/f.yaml"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	}
	files := openapitest.SpecFiles(t, root)
	got := make([]string, 0, len(files))
	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		require.NoError(t, err)
		got = append(got, filepath.ToSlash(rel))
	}
	assert.Equal(t, []string{"a.yaml", "b.YML", "c.json", "sub/f.yaml"}, got)
}

// TestSpecFiles_FailsWhereItFindsNoSpec pins that a sweep reading nothing
// fails rather than passing vacuously: a root holding no spec, and one that
// does not exist.
func TestSpecFiles_FailsWhereItFindsNoSpec(t *testing.T) {
	t.Parallel()
	for _, root := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		r := &recorder{}
		assert.Empty(t, openapitest.SpecFiles(r, root), root)
		assert.True(t, r.failed(), root)
	}
}

// positionsDoc has a schema the model's walk reaches, a path whose key needs
// escaping, an extension only the tree reaches, and a response written as a
// reference, internal and external, and one naming a key that holds a plus.
const positionsDoc = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {responses: {"200": {description: ok}, "201": {$ref: '#/components/responses/R'}, "202": {$ref: 'other.yaml#/R'}, "203": {$ref: '#anchor'}, "204": {$ref: '#/paths/~1b+c'}}}}
  /b+c: {x-p: {k: v}}
components:
  schemas:
    A: {type: object}
  responses:
    R: {description: r, x-k: {a: 1}}
x-ext: {k: [v]}
`

// TestPositions_ReadsTheWalkTheTreeAndAStrayToken pins what a differential is
// asked about: a place the walk reaches, a path only the tree has, an escaped
// key, a list index, a path of an internal reference's target read through
// it, a plus in the key it names included, though not of an external one's nor
// an anchor's, and each with a stray token appended.
func TestPositions_ReadsTheWalkTheTreeAndAStrayToken(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(positionsDoc))
	require.NoError(t, err)
	got := openapitest.Positions(t.Context(), t, doc)
	for _, want := range []jsontext.Pointer{
		"/components/schemas/A", "/x-ext/k/0", "/paths/~1a/get", "/x-ext/k/0/0", "/components/schemas/A/properties",
		"/paths/~1a/x", "/x-ext/", "//~1",
		"/paths/~1a/get/responses/201/x-k/a", "/paths/~1a/get/responses/201/description/0",
		"/paths/~1a/get/responses/204/x-p/k",
	} {
		assert.Contains(t, got, want)
	}
	for _, p := range got {
		for _, unread := range []string{"/paths/~1a/get/responses/202/", "/paths/~1a/get/responses/203/"} {
			assert.False(t, strings.HasPrefix(string(p), unread+"description"), "%q: no target is read there", p)
		}
	}
	seen := map[jsontext.Pointer]bool{}
	for _, p := range got {
		assert.False(t, seen[p], "%q is asked once", p)
		seen[p] = true
	}
}

// TestPositions_ReadsAnAliasAsTheScalarItNames pins that a key spelled as an
// alias is asked about under the key it reads as, and that a $ref with its key
// or its value spelled so is read through like any other, since the anchor's
// name is no position and no reference.
func TestPositions_ReadsAnAliasAsTheScalarItNames(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-names: [&spelled spdx, &reference $ref, &target '#/x-target']
x-target: {below: 1}
x-license: {*spelled : MIT}
x-link: {*reference : '#/x-target'}
x-value: {$ref: *target}
`))
	require.NoError(t, err)
	got := openapitest.Positions(t.Context(), t, doc)
	assert.Contains(t, got, jsontext.Pointer("/x-license/spdx"), "the key it reads as")
	assert.NotContains(t, got, jsontext.Pointer("/x-license/spelled"), "not the anchor's name")
	assert.Contains(t, got, jsontext.Pointer("/x-link/below"), "the target's path, read through the $ref")
	assert.Contains(t, got, jsontext.Pointer("/x-value/below"), "and through a $ref whose value is an alias")
}

// TestPositions_FailsPastMaxTreeDepth pins the bound on the tree's depth: a
// spec nesting past it fails the test and yields nothing, rather than leaving
// the paths below unread.
func TestPositions_FailsPastMaxTreeDepth(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("{a: ", openapitest.MaxTreeDepth) + "v" + strings.Repeat("}", openapitest.MaxTreeDepth)
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(positionsDoc+"x-deep: "+deep+"\n"))
	require.NoError(t, err)

	r := &recorder{}
	assert.Nil(t, openapitest.Positions(t.Context(), r, doc))
	assert.True(t, r.failed())

	shallow, _, err := soa.Unmarshal(t.Context(), strings.NewReader(positionsDoc))
	require.NoError(t, err)
	r = &recorder{}
	assert.NotEmpty(t, openapitest.Positions(t.Context(), r, shallow))
	assert.False(t, r.failed())
}

// TestPositions_ReadsAModelWithNoTree pins a model built in code, which has no
// tree to read: its walk's places are asked about all the same.
func TestPositions_ReadsAModelWithNoTree(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	got := openapitest.Positions(t.Context(), r, openapitest.DocDeclaring("A"))
	assert.Contains(t, got, jsontext.Pointer("/components/schemas/A"))
	assert.False(t, r.failed())
}
