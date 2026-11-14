// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"reflect"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestReferenceKinds_EveryKindTheLibraryHasIsRead pins that the one place this
// package lists the library's kinds of reference object, refKindOf, and the
// decoding of each (decodeObject), name every one of them. The kinds are read
// off the walk's own matcher, so one the library comes to hold reddens this
// until the source holds a reference of it: a default arm would otherwise read
// it as no reference at all, which the bindings would never resolve. A schema
// is none of them: the compiler binds a schema's $ref as a schema's.
func TestReferenceKinds_EveryKindTheLibraryHasIsRead(t *testing.T) {
	t.Parallel()
	want := libraryReferenceKinds(t)
	delete(want, reflect.TypeFor[*schemaRef]())

	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(openapitest.EveryKindOfReference)},
		Options{})
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)

	seen := map[reflect.Type]bool{}
	for item := range soa.Walk(t.Context(), got.Doc) {
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			r, ok := model.(interface {
				IsReference() bool
				GetCoreAny() any
			})
			if !ok || !r.IsReference() || !want[reflect.TypeOf(model)] {
				return nil
			}
			name := reflect.TypeOf(model).String()
			seen[reflect.TypeOf(model)] = true
			kind, known := refKindOf(model)
			require.True(t, known, "%s: the load phase reads it as a reference object", name)
			ref, bound := got.Bindings.Reference(model)
			require.True(t, bound, "%s: the bindings know where it is written", name)
			assert.Equal(t, bind.Resolved, ref.Chain.Status, "%s: and where its chain ends", name)
			core, _ := r.GetCoreAny().(interface{ GetRootNode() *yaml.Node })
			built, _, err := decodeObject(t.Context(), kind, core.GetRootNode())
			require.NoError(t, err, name)
			assert.Equal(t, reflect.TypeOf(model), reflect.TypeOf(built), "%s: an object of its kind is built", name)
			return nil
		}})
	}
	assert.Equal(t, want, seen, "the source holds a resolved reference of each kind")
	_, known := refKindOf(got.Doc)
	assert.False(t, known, "a value of no reference kind is none")
}

// libraryReferenceKinds returns each kind of reference the library has: those
// the walk's own matcher names, and the schema.
func libraryReferenceKinds(t *testing.T) map[reflect.Type]bool {
	t.Helper()
	kinds := map[reflect.Type]bool{reflect.TypeFor[*schemaRef](): true}
	for field := range reflect.TypeFor[soa.Matcher]().Fields() {
		if strings.HasPrefix(field.Name, "Referenced") {
			kinds[field.Type.In(0)] = true
		}
	}
	require.GreaterOrEqual(t, len(kinds), 10, "every kind of reference the matcher names, and the schema's")
	return kinds
}
