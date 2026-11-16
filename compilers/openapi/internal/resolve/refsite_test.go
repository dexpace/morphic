// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// schemaRef is the parser's schema value a component holds.
type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// bound is a loaded source with the scope lowering reads it in.
type bound struct {
	doc   *load.Document
	scope resolve.Scope
}

// loadBound loads src as the source at path and returns it with a scope over
// its bindings. Load reports the $refs src leaves unresolved, and those are
// the fixtures, so its diagnostics are not asserted here.
func loadBound(t *testing.T, path, src string, opts load.Options) bound {
	t.Helper()
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(src)}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.NotNil(t, doc.Bindings, "the source is bound")
	return bound{doc: doc, scope: resolve.Scope{SelfPath: path, Bindings: doc.Bindings}}
}

// component returns the component schema called name.
func (b bound) component(t *testing.T, name string) *schemaRef {
	t.Helper()
	js, ok := b.doc.Doc.Components.Schemas.Get(name)
	require.True(t, ok, "the fixture declares component %q", name)
	return js
}

// externalDocument writes ext.yaml beside a spec in a directory of its own,
// its links resolved as a compile names the files in it, and returns the path
// the spec is loaded by.
func externalDocument(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	body := "components:\n  schemas:\n    E: {type: string, title: external}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(body), 0o600))
	return filepath.Join(dir, "spec.yaml")
}

func TestResolveComponentRef(t *testing.T) {
	t.Parallel()
	sc := resolve.Scope{Declares: resolve.NewNames("User")}

	id, state := sc.ComponentRef("/components/schemas/User")
	assert.Equal(t, ids.NamedComponent, state)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/User"), id)

	_, state = sc.ComponentRef("/components/schemas/Missing")
	assert.Equal(t, ids.UndeclaredComponent, state, "a name the document does not declare does not resolve")

	_, state = sc.ComponentRef("/components/schemas/")
	assert.Equal(t, ids.UnnamedComponent, state, "the component keyed \"\" is its own state, never folded into another")

	_, state = sc.ComponentRef("/components/schemas/Foo/properties/bar")
	assert.Equal(t, ids.NotComponent, state, "a sub-schema pointer is not a top-level component pointer")
}

// TestResolveComponentRef_NonCanonicalEscape pins that the resolved ID is built
// from the component's canonical name, so a $ref that escapes non-canonically
// (a raw '~' for a component named "A~B", interned under "A~0B") still resolves to
// the interned node rather than an unbacked ID (issue #14).
func TestResolveComponentRef_NonCanonicalEscape(t *testing.T) {
	t.Parallel()
	sc := resolve.Scope{Declares: resolve.NewNames("A~B")}

	id, state := sc.ComponentRef("/components/schemas/A~B")
	assert.Equal(t, ids.NamedComponent, state)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/A~0B"), id,
		"the ID is canonically re-escaped to match the interned node")
	assert.Equal(t, ids.Document{}.NamedType(ids.Ptr("components", "schemas", "A~B")), id,
		"and equals the ID the component was interned under")
}
