package load

import (
	"reflect"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestReferenceKinds_EveryKindTheLibraryHasIsRead pins that the places this
// package and the lowering list the library's kinds of reference object, each
// of which reads a reference through its own type, name every one of them. The
// kinds are read off the walk's own matcher, so one the library comes to hold
// reddens this until the source holds a reference of it: a default arm
// would otherwise read it as no reference at all. A schema is none of them: the
// compiler binds a schema's $ref itself.
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
			r, ok := model.(resolvable)
			if !ok || !r.IsReference() || !r.IsResolved() {
				return nil
			}
			if !want[reflect.TypeOf(model)] {
				return nil
			}
			name := reflect.TypeOf(model).String()
			seen[reflect.TypeOf(model)] = true
			_, reached := resolutionChainEnd(model)
			assert.True(t, reached, "%s: the load phase reads where its chain ends", name)
			_, ends := refscope.ReferenceEnd(model)
			assert.True(t, ends, "%s: the lowering reads where its chain ends", name)
			_, stands := emptyOf(r)
			assert.True(t, stands, "%s: a stand-in is held for each kind", name)
			_, _, copies := refCopies(model)
			assert.True(t, copies, "%s: load puts back a $ref it respelled", name)
			return nil
		}})
	}
	assert.Equal(t, want, seen, "the source holds a resolved reference of each kind")
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

// resolutionChainEnd reports the last record model's chain holds.
func resolutionChainEnd(model any) (record, bool) {
	records := resolutionChain(model).records
	if len(records) == 0 {
		return record{}, false
	}
	return records[len(records)-1], true
}
