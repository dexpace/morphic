package load

import (
	"reflect"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestReferenceKinds_EveryKindTheLibraryHasIsRead pins that the places this
// package and the lowering list the library's kinds of reference, each of which
// reads a reference through its own type, name every one of them. The kinds
// are read off the walk's own matcher, so one the library comes to hold
// reddens this until the source holds a reference of it: a default arm
// would otherwise read it as no reference at all.
func TestReferenceKinds_EveryKindTheLibraryHasIsRead(t *testing.T) {
	t.Parallel()
	want := map[reflect.Type]bool{reflect.TypeFor[*schemaRef](): true}
	matcher := reflect.TypeFor[soa.Matcher]()
	for i := range matcher.NumField() {
		field := matcher.Field(i)
		if strings.HasPrefix(field.Name, "Referenced") {
			want[field.Type.In(0)] = true
		}
	}
	require.GreaterOrEqual(t, len(want), 10, "every kind of reference the matcher names, and the schema's")

	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(openapitest.EveryKindOfReference)},
		Options{})
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)

	seen := map[reflect.Type]bool{}
	for item := range soa.Walk(t.Context(), got.Doc) {
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			r, ok := model.(resolvable)
			if !ok || !r.IsReference() || !r.IsResolved() {
				return nil
			}
			if _, isSchema := model.(*oas3.JSONSchema[oas3.Referenceable]); !isSchema && !want[reflect.TypeOf(model)] {
				return nil
			}
			name := reflect.TypeOf(model).String()
			seen[reflect.TypeOf(model)] = true
			_, reached := resolutionChainEnd(model)
			assert.True(t, reached, "%s: the load phase reads where its chain ends", name)
			_, ends := refscope.ReferenceEnd(model)
			assert.True(t, ends, "%s: the lowering reads where its chain ends", name)
			_, stands := emptyOf(r)
			assert.Equal(t, !isSchemaModel(model), stands, "%s: a stand-in is held for each kind but the schema", name)
			return nil
		}})
	}
	assert.Equal(t, want, seen, "the source holds a resolved reference of each kind")
}

// isSchemaModel reports whether model is a schema.
func isSchemaModel(model any) bool {
	_, ok := model.(*schemaRef)
	return ok
}

// resolutionChainEnd reports the last record model's chain reached.
func resolutionChainEnd(model any) (record, bool) {
	var last record
	reached := false
	for _, r := range resolutionChain(model).records {
		if r.reached {
			last, reached = r, true
		}
	}
	return last, reached
}
